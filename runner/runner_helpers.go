package runner

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chenasraf/vstask/tasks"
)

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
	// Quote if it has whitespace or shell metachars (including quotes).
	if containsAnyRunes(s, " \t\n\r;&|()<>[]{}*?!~`$\\\"'") {
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
