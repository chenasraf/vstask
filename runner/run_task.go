package runner

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
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
	type key struct{ label string }
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Separate process group (Unix) so we can kill children too.
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

// ------------ helpers ------------

func indexByLabel(ts []tasks.Task) map[string]tasks.Task {
	m := make(map[string]tasks.Task, len(ts))
	for _, t := range ts {
		m[t.Label] = t
	}
	return m
}

func applyPlatformOverrides(t tasks.Task) tasks.Task {
	eff := t
	switch runtime.GOOS {
	case "windows":
		if t.Windows != nil {
			if t.Windows.Command != "" {
				eff.Command = t.Windows.Command
			}
			if t.Windows.Args != nil {
				eff.Args = append([]string(nil), t.Windows.Args...)
			}
			if t.Windows.Options != nil {
				eff.Options = t.Windows.Options
			}
			if t.Windows.Presentation != nil {
				eff.Presentation = t.Windows.Presentation
			}
		}
	case "darwin":
		if t.Osx != nil {
			if t.Osx.Command != "" {
				eff.Command = t.Osx.Command
			}
			if t.Osx.Args != nil {
				eff.Args = append([]string(nil), t.Osx.Args...)
			}
			if t.Osx.Options != nil {
				eff.Options = t.Osx.Options
			}
			if t.Osx.Presentation != nil {
				eff.Presentation = t.Osx.Presentation
			}
		}
	case "linux":
		if t.Linux != nil {
			if t.Linux.Command != "" {
				eff.Command = t.Linux.Command
			}
			if t.Linux.Args != nil {
				eff.Args = append([]string(nil), t.Linux.Args...)
			}
			if t.Linux.Options != nil {
				eff.Options = t.Linux.Options
			}
			if t.Linux.Presentation != nil {
				eff.Presentation = t.Linux.Presentation
			}
		}
	}
	return eff
}

func substituteVars(s string, vars map[string]string) string {
	if s == "" {
		return s
	}
	out := s
	for k, v := range vars {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
	}
	return out
}

func mergeEnv(base []string, extra map[string]string) []string {
	// Convert base to map
	m := map[string]string{}
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	maps.Copy(m, extra)
	// Back to slice
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func buildCmd(t tasks.Task, cwd string, env []string) (*exec.Cmd, func(), error) {
	cleanup := func() {}
	typ := strings.ToLower(strings.TrimSpace(t.Type))
	if typ == "" {
		typ = "shell" // VS Code default
	}

	switch typ {
	case "process":
		if t.Command == "" {
			return nil, cleanup, errors.New("process task has empty command")
		}
		cmd := exec.Command(t.Command, t.Args...)
		cmd.Dir = cwd
		cmd.Env = env
		return cmd, cleanup, nil

	case "shell":
		shExe, shArgs := defaultShell()
		if t.Options != nil && t.Options.Shell != nil && t.Options.Shell.Executable != "" {
			shExe = t.Options.Shell.Executable
			if len(t.Options.Shell.Args) > 0 {
				shArgs = append([]string(nil), t.Options.Shell.Args...)
			}
		}

		// Build a single command line for the shell.
		line := buildCommandLine(t.Command, t.Args)
		args := append([]string{}, shArgs...)
		args = append(args, line)

		cmd := exec.Command(shExe, args...)
		cmd.Dir = cwd
		cmd.Env = env
		return cmd, cleanup, nil

	default:
		return nil, cleanup, fmt.Errorf("unsupported task type: %q", t.Type)
	}
}

func defaultShell() (exe string, args []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/C"}
	}
	// Prefer bash if present? Keeping /bin/sh for portability.
	return "/bin/sh", []string{"-c"}
}

func buildCommandLine(cmd string, args []string) string {
	if runtime.GOOS == "windows" {
		parts := make([]string, 0, 1+len(args))
		if cmd != "" {
			parts = append(parts, winQuote(cmd))
		}
		for _, a := range args {
			parts = append(parts, winQuote(a))
		}
		return strings.Join(parts, " ")
	}

	// POSIX: prefer double-quoting so $(...) and $VAR still expand.
	if len(args) == 0 {
		// Let shell parse/expand everything in command (e.g., $(...), pipes, etc.)
		return cmd
	}
	var b strings.Builder
	if cmd != "" {
		b.WriteString(cmd) // verbatim, preserves expansions in command
	}
	for _, a := range args {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(posixQuoteForShell(a)) // quote only args
	}
	return b.String()
}

func posixQuoteForShell(s string) string {
	if s == "" {
		return `""`
	}
	// If it already contains unbalanced quotes, assume the user knows what they're doing.
	if strings.ContainsRune(s, '\'') || strings.Count(s, `"`)%2 == 1 {
		return s
	}
	// Quote if it has whitespace or shell metachars.
	if containsAnyRunes(s, " \t\n\r;&|()<>[]{}*?!~`$\\\"") {
		// Escape backslashes and double quotes inside double quotes.
		esc := strings.ReplaceAll(s, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		return `"` + esc + `"`
	}
	return s
}

func containsAnyRunes(s, set string) bool {
	for _, r := range s {
		if strings.ContainsRune(set, r) {
			return true
		}
	}
	return false
}

func winQuote(s string) string {
	// Very light quoting good enough for cmd.exe /C
	if s == "" {
		return `""`
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return r <= ' ' || strings.ContainsRune(`"^&|<>()%!`, r)
	}) >= 0 {
		// escape " by doubling
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func killTree(p *os.Process) {
	if p == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// Best-effort kill process tree on Windows.
		// Requires taskkill to be present (it is on standard Windows).
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", p.Pid)).Run()
		return
	}
	// Unix: kill the whole process group.
	_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	// If still alive after a moment, SIGKILL.
	time.AfterFunc(1*time.Second, func() { _ = syscall.Kill(-p.Pid, syscall.SIGKILL) })
}

func mustGetwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	// Fallback to HOME if Getwd fails
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// Same as buildVSCodeVarMap, but lets you override ${cwd} with the task's effective cwd.
func buildVSCodeVarMapWithCWD(workspace, cwd string) map[string]string {
	// Start with your existing builder
	vars := buildVSCodeVarMap(workspace)
	if cwd != "" {
		vars["cwd"] = cwd
	}
	return vars
}

// buildVSCodeVarMap constructs all built-in VS Code substitutions.
// Many editor-specific values are best-effort via env fallbacks.
func buildVSCodeVarMap(workspace string) map[string]string {
	vars := map[string]string{}

	// ${userHome}
	if home, err := os.UserHomeDir(); err == nil {
		vars["userHome"] = home
	}

	// ${workspaceFolder}, ${workspaceFolderBasename}
	if workspace != "" {
		vars["workspaceFolder"] = workspace
		vars["workspaceFolderBasename"] = filepath.Base(workspace)
	}

	// ${cwd}  (best effort: current process dir)
	if wd, err := os.Getwd(); err == nil {
		vars["cwd"] = wd
	}

	// ${execPath} (best effort: env or 'code' on PATH)
	if v := os.Getenv("VSCODE_EXEC_PATH"); v != "" {
		vars["execPath"] = v
	} else if p, _ := exec.LookPath("code"); p != "" {
		vars["execPath"] = p
	}

	// ${defaultBuildTask} (scan tasks)
	if all, err := tasks.GetTasks(); err == nil {
		for _, t := range all {
			if t.Group != nil && strings.EqualFold(t.Group.Kind, "build") && t.Group.IsDefault {
				vars["defaultBuildTask"] = t.Label
				break
			}
		}
	}

	// ${pathSeparator} and ${/}
	sep := string(os.PathSeparator)
	vars["pathSeparator"] = sep
	vars["/"] = sep

	return vars
}
