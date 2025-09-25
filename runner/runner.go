package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
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
				if err := runTaskInternal(dep, root, resolver, true); err != nil {
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
				go func(tp tasks.Task, name string) {
					defer wg.Done()
					if err := runTaskInternal(tp, root, resolver, true); err != nil {
						errCh <- fmt.Errorf("dependency %q failed: %w", name, err)
					}
				}(dep, depLbl)
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

	// Now run the main task fully (i.e., wait for process exit).
	return runTaskInternal(task, root, resolver, false /* waitForReady */)
}

// ----- Internal helpers -----

// startAndWaitReady starts cmd, mirrors output to the user's terminal, and:
//   - if bg == nil: waits for process exit and returns its error (normal task).
//   - if bg != nil and waitForReady == true: returns when "ready" (ActiveOnStart or BeginsRx match).
//     The process continues running in the background.
//   - if bg != nil and waitForReady == false: behaves like a normal task (waits for exit).
func startAndWaitReady(ctx context.Context, cmd *execCmdShim, interactive bool, bg *tasks.BgMatcher, waitForReady bool) error {
	// If no background matcher is involved, defer to the existing path (PTY where possible).
	if bg == nil || !waitForReady {
		return startAndWait(ctx, cmd.Cmd, interactive)
	}

	// For readiness-gated deps we need to *observe* stdout/stderr to detect patterns.
	// We'll run WITHOUT PTY here (interactive=false) so we can pipe and scan reliably.
	stdout, err := cmd.Cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.Cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Cmd.Start(); err != nil {
		return err
	}

	readyCh := make(chan struct{})
	once := sync.Once{}

	// Echo+scan a single stream.
	scan := func(r io.Reader, w io.Writer) {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				// Mirror to user terminal
				_, _ = io.WriteString(w, line)
				// Check patterns for readiness
				if bg != nil {
					if bg.ActiveOnStart {
						once.Do(func() { close(readyCh) })
					} else if bg.BeginsRx != nil && bg.BeginsRx.MatchString(line) {
						once.Do(func() { close(readyCh) })
					}
					// EndsRx is informative for cycles; not required to signal readiness.
				}
			}
			if err != nil {
				return
			}
		}
	}

	// Stream both pipes
	go scan(stdout, os.Stdout)
	go scan(stderr, os.Stderr)

	// If ActiveOnStart is set, the scanner will close readyCh immediately on first read loop tick.
	// However, ensure we don't hang in case the tool prints nothing at all: still rely on ActiveOnStart.

	// Wait until the context is done, process exits, or we become "ready"
	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = terminateProcessTree(cmd.Cmd)
		<-waitErrCh
		return ctx.Err()
	case err := <-waitErrCh:
		// Process exited before readiness; for a dep this means failure/finish.
		return err
	case <-readyCh:
		// Deps: we are ready; do NOT wait for exit. Let it keep running.
		// NOTE: we intentionally DO NOT return the eventual exit code.
		return nil
	}
}

func runTaskInternal(t tasks.Task, workspace string, resolver *InputResolver, waitForReady bool) error {
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

	// Build the command and a cleanup hook
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

	// Extract background matcher (if any)
	bg := extractBgMatcher(eff)

	// If we need to waitForReady and we have a background matcher, run readiness-gated mode.
	// Otherwise use the standard startAndWait (PTY-enabled).
	if bg != nil && waitForReady {
		// We launch in stream/pipe mode to observe output; PTY is skipped for reliability.
		return startAndWaitReady(ctx, &execCmdShim{Cmd: cmd}, false, bg, true)
	}

	// Normal path: try interactive (PTY) first if possible; else stdio.
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

// Background readiness matcher (VS Code parity)
func extractBgMatcher(t tasks.Task) *tasks.BgMatcher {
	// Must be a background task AND have a background problem matcher.
	if !t.IsBackground || t.ProblemMatcher == nil {
		return nil
	}

	bg := t.ProblemMatcher.FirstBackground()
	if bg == nil {
		// No usable background config → no readiness gating (VS Code also can't detect readiness)
		return nil
	}

	var beginsRx, endsRx *regexp.Regexp
	if s := strings.TrimSpace(bg.BeginsPattern); s != "" {
		if rx, err := regexp.Compile(s); err == nil {
			beginsRx = rx
		}
	}
	if s := strings.TrimSpace(bg.EndsPattern); s != "" {
		if rx, err := regexp.Compile(s); err == nil {
			endsRx = rx
		}
	}

	// If we can't detect readiness at all, bail out (matches VS Code behavior).
	if !bg.ActiveOnStart && beginsRx == nil {
		return nil
	}

	return &tasks.BgMatcher{
		ActiveOnStart: bg.ActiveOnStart,
		BeginsRx:      beginsRx,
		EndsRx:        endsRx,
	}
}

type execCmdShim struct {
	Cmd *exec.Cmd
}
