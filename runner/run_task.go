package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chenasraf/vstask/tasks"
	"github.com/chenasraf/vstask/utils"
)

func RunTask(task tasks.Task) error {
	// Load all tasks so we can resolve dependsOn by label.
	all, err := tasks.GetTasks()
	if err != nil {
		return err
	}
	index := indexByLabel(all)

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
				if err := runSingleTaskWithDeps(dep, index, root); err != nil {
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
					if err := runSingleTaskWithDeps(dep, index, root); err != nil {
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
	return runSingleTask(task, root)
}

func runSingleTaskWithDeps(t tasks.Task, index map[string]tasks.Task, root string) error {
	// Avoid infinite recursion if someone misconfigured cyclic deps.
	seen := map[string]bool{}
	var run func(tasks.Task) error
	run = func(tt tasks.Task) error {
		if seen[tt.Label] {
			return fmt.Errorf("cycle detected at %q", tt.Label)
		}
		seen[tt.Label] = true
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
		return runSingleTask(tt, root)
	}
	return run(t)
}

func runSingleTask(t tasks.Task, workspace string) error {
	eff := applyPlatformOverrides(t)

	// Prelim vars (process cwd)
	preVars := buildVSCodeVarMapWithCWD(workspace, mustGetwd())

	// Resolve the task's effective cwd
	cwd := workspace
	if eff.Options != nil && eff.Options.Cwd != "" {
		cwdr := substituteVars(eff.Options.Cwd, preVars)
		if filepath.IsAbs(cwdr) {
			cwd = cwdr
		} else {
			cwd = filepath.Join(workspace, cwdr)
		}
	}

	// Final vars with the effective cwd
	vars := buildVSCodeVarMapWithCWD(workspace, cwd)

	// Substitute ${...} in command/args using the final vars
	eff.Command = substituteVars(eff.Command, vars)
	for i := range eff.Args {
		eff.Args[i] = substituteVars(eff.Args[i], vars)
	}

	// Environment
	env := os.Environ()
	if eff.Options != nil && len(eff.Options.Env) > 0 {
		merged := make(map[string]string, len(eff.Options.Env))
		for k, v := range eff.Options.Env {
			merged[k] = substituteVars(v, vars)
		}
		env = mergeEnv(env, merged)
	}

	// Build exec.Cmd depending on task type (shell/process).
	cmd, cleanup, err := buildCmd(eff, cwd, env)
	if err != nil {
		return err
	}
	defer cleanup()

	// Wire stdio for interactive tasks.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Make a context that cancels on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), trapSignals()...)
	defer stop()

	// Separate process group (Unix) so we can kill children too.
	if runtime.GOOS != "windows" {
		setProcessGroup(cmd)
	}

	// Start
	fmt.Printf("Running task: %s\n", t.Label)
	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait in a goroutine so we can cancel.
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		// Signal received; kill process tree.
		killTree(cmd.Process)
		// Give it a moment to die gracefully.
		select {
		case err := <-waitErr:
			return err
		case <-time.After(2 * time.Second):
			return errors.New("killed")
		}
	case err := <-waitErr:
		return err
	}
}
