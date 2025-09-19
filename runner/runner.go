package runner

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/chenasraf/vstask/tasks"
	"github.com/chenasraf/vstask/utils"
)

// RunTask executes a task, resolving its dependsOn (sequence/parallel) and prompting for ${input:*}.
func RunTask(task tasks.Task) error {
	// Load all tasks so we can resolve dependsOn by label.
	all, err := tasks.GetTasks()
	if err != nil {
		return err
	}
	index := indexByLabel(all)

	// Load inputs (best effort; if not present we'll fallback to generic prompting).
	var inputs []tasks.Input
	if gi, err := tasks.GetInputs(); err == nil && gi != nil {
		inputs = gi
	}
	resolver := NewInputResolver(inputs)

	// Figure out workspace folder for substitutions.
	root, err := utils.FindProjectRoot()
	if err != nil {
		return err
	}

	// Execute dependencies (if any), then this task.
	if task.DependsOn != nil && len(task.DependsOn.Tasks) > 0 {
		switch strings.ToLower(task.DependsOrder) {
		case "sequence":
			for _, lbl := range task.DependsOn.Tasks {
				dep, ok := index[lbl]
				if !ok {
					return fmt.Errorf("dependsOn: task %q not found", lbl)
				}
				if err := runSingleTaskWithDeps(dep, index, root, resolver); err != nil {
					return fmt.Errorf("dependency %q failed: %w", lbl, err)
				}
			}
		default: // parallel is VS Code's default
			var wg sync.WaitGroup
			errCh := make(chan error, len(task.DependsOn.Tasks))
			for _, lbl := range task.DependsOn.Tasks {
				depLbl := lbl
				dep, ok := index[depLbl]
				if !ok {
					return fmt.Errorf("dependsOn: task %q not found", depLbl)
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := runSingleTaskWithDeps(dep, index, root, resolver); err != nil {
						errCh <- fmt.Errorf("dependency %q failed: %w", depLbl, err)
					}
				}()
			}
			wg.Wait()
			close(errCh)
			for e := range errCh {
				if e != nil {
					return e
				}
			}
		}
	}

	// Now run the main task.
	return runSingleTask(task, root, resolver)
}

func runSingleTask(t tasks.Task, workspace string, resolver *InputResolver) error {
	eff := applyPlatformOverrides(t)

	// ---- Prompt for all inputs referenced by this effective task BEFORE doing anything else ----
	promptInputsForTask(eff, resolver)

	// Prelim vars (process cwd)
	preVars := buildVSCodeVarMapWithCWD(workspace, mustGetwd())

	// Resolve the task's effective cwd (support ${input:*} + ${vscodeVar})
	cwd := workspace
	if eff.Options != nil && eff.Options.Cwd != "" {
		cwdr := replaceInputs(eff.Options.Cwd, resolver)
		cwdr = substituteVars(cwdr, preVars)
		if filepath.IsAbs(cwdr) {
			cwd = cwdr
		} else {
			cwd = filepath.Join(workspace, cwdr)
		}
	}

	// Final vars with the effective cwd
	vars := buildVSCodeVarMapWithCWD(workspace, cwd)

	// Substitute inputs then vscode vars in command/args
	eff.Command = replaceInputs(eff.Command, resolver)
	eff.Command = substituteVars(eff.Command, vars)

	for i := range eff.Args {
		eff.Args[i] = replaceInputs(eff.Args[i], resolver)
		eff.Args[i] = substituteVars(eff.Args[i], vars)
	}

	// Environment
	env := os.Environ()
	if eff.Options != nil && len(eff.Options.Env) > 0 {
		merged := make(map[string]string, len(eff.Options.Env))
		for k, v := range eff.Options.Env {
			val := replaceInputs(v, resolver)
			val = substituteVars(val, vars)
			merged[k] = val
		}
		env = mergeEnv(env, merged)
	}

	cmd, cleanup, err := buildCmd(eff, cwd, env)
	if err != nil {
		return err
	}
	defer cleanup()

	// Make a context that cancels on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), trapSignals()...)
	defer stop()

	// Separate process group (Unix) so we can kill children too.
	if runtime.GOOS != "windows" {
		setProcessGroup(cmd)
	}

	fmt.Printf("Running task: %s\n", t.Label)

	// Try interactive (PTY) first if possible; else stdio.
	err = startAndWait(ctx, cmd, true)
	if err == nil {
		return nil
	}
	// If bash was blocked, retry with /bin/sh
	if shouldFallbackToSh(cmd, err) {
		if shCmd := rebuildWithSh(cmd); shCmd != nil {
			return startAndWait(ctx, shCmd, true)
		}
	}
	return err
}

func runSingleTaskWithDeps(t tasks.Task, index map[string]tasks.Task, root string, resolver *InputResolver) error {
	// Avoid infinite recursion if someone misconfigured cyclic deps.
	seen := map[string]bool{}
	var run func(tasks.Task) error
	run = func(tt tasks.Task) error {
		if seen[tt.Label] {
			return fmt.Errorf("cycle detected at %q", tt.Label)
		}
		seen[tt.Label] = true

		// Prompt inputs early for this node in the graph
		promptInputsForTask(applyPlatformOverrides(tt), resolver)

		if tt.DependsOn != nil && len(tt.DependsOn.Tasks) > 0 {
			switch strings.ToLower(tt.DependsOrder) {
			case "sequence":
				for _, lbl := range tt.DependsOn.Tasks {
					dep, ok := index[lbl]
					if !ok {
						return fmt.Errorf("dependsOn: task %q not found", lbl)
					}
					if err := run(dep); err != nil {
						return err
					}
				}
			default:
				var wg sync.WaitGroup
				errCh := make(chan error, len(tt.DependsOn.Tasks))
				for _, lbl := range tt.DependsOn.Tasks {
					dep, ok := index[lbl]
					if !ok {
						return fmt.Errorf("dependsOn: task %q not found", lbl)
					}
					wg.Add(1)
					go func(depTask tasks.Task) {
						defer wg.Done()
						if err := run(depTask); err != nil {
							errCh <- err
						}
					}(dep)
				}
				wg.Wait()
				close(errCh)
				for e := range errCh {
					if e != nil {
						return e
					}
				}
			}
		}
		return runSingleTask(tt, root, resolver)
	}
	return run(t)
}
