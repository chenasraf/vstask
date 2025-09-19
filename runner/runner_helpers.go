package runner

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/chenasraf/vstask/tasks"
	"github.com/manifoldco/promptui"
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

// ----------------- Input resolution -----------------

// Expectation for tasks.Input (align with your tasks package):
// type Input struct {
//   ID          string   `json:"id"`
//   Type        string   `json:"type"` // "promptString" | "pickString" | "command"
//   Description string   `json:"description"`
//   Default     string   `json:"default"`
//   Password    bool     `json:"password"` // promptString only
//   Options     []string `json:"options"`   // pickString only
//   Command     string   `json:"command"`   // command only
// }

type InputResolver struct {
	byID  map[string]tasks.Input
	cache map[string]string
}

func NewInputResolver(inputs []tasks.Input) *InputResolver {
	m := make(map[string]tasks.Input, len(inputs))
	for _, in := range inputs {
		m[in.ID] = in
	}
	return &InputResolver{
		byID:  m,
		cache: map[string]string{},
	}
}

var reInput = regexp.MustCompile(`\$\{input:([^}]+)\}`)

// promptInputsForTask scans the effective task for ${input:*} and resolves all before running.
func promptInputsForTask(t tasks.Task, r *InputResolver) {
	ids := collectInputRefsFromTask(t)
	for _, id := range ids {
		_, _ = r.Resolve(id) // cache it
	}
}

func collectInputRefsFromTask(t tasks.Task) []string {
	seen := make(map[string]struct{})

	grab := func(s string) {
		for _, m := range reInput.FindAllStringSubmatch(s, -1) {
			if len(m) == 2 {
				seen[m[1]] = struct{}{}
			}
		}
	}

	grab(t.Command)
	for _, a := range t.Args {
		grab(a)
	}
	if t.Options != nil {
		grab(t.Options.Cwd)
		for _, v := range t.Options.Env {
			grab(v)
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

func replaceInputs(s string, r *InputResolver) string {
	if s == "" || r == nil {
		return s
	}
	return reInput.ReplaceAllStringFunc(s, func(m string) string {
		sub := reInput.FindStringSubmatch(m)
		if len(sub) == 2 {
			val, _ := r.Resolve(sub[1])
			return val
		}
		return m
	})
}

// Resolve returns a value for an input id, prompting if necessary.
// Caches values so the same id is only prompted once.
func (r *InputResolver) Resolve(id string) (string, error) {
	if v, ok := r.cache[id]; ok {
		return v, nil
	}

	// Env override (handy for CI): VSTASK_INPUT_<UPPER_ID>
	if env := os.Getenv("VSTASK_INPUT_" + strings.ToUpper(id)); env != "" {
		r.cache[id] = env
		return env, nil
	}

	in, ok := r.byID[id]
	if !ok {
		// Unknown input: fallback to simple line prompt.
		val, err := simpleLinePrompt(fmt.Sprintf("Enter value for %s", id), "")
		if err != nil {
			return "", err
		}
		r.cache[id] = val
		return val, nil
	}

	switch strings.ToLower(in.Type) {
	case "promptstring":
		lbl := in.Description
		if strings.TrimSpace(lbl) == "" {
			lbl = fmt.Sprintf("Enter %s", in.ID)
		}
		val, err := promptString(lbl, in.Default, in.Password)
		if err != nil {
			return "", err
		}
		r.cache[id] = val
		return val, nil

	case "pickstring":
		if len(in.Options) == 0 {
			// Degenerate case: no options → line prompt with default
			lbl := in.Description
			if strings.TrimSpace(lbl) == "" {
				lbl = fmt.Sprintf("Enter %s", in.ID)
			}
			val, err := promptString(lbl, in.Default, false)
			if err != nil {
				return "", err
			}
			r.cache[id] = val
			return val, nil
		}
		val, err := promptSelect(in.DescriptionOrFallback(), in.Options, in.Default)
		if err != nil {
			return "", err
		}
		r.cache[id] = val
		return val, nil

	case "command":
		out := strings.TrimSpace(runInputShell(in.Command))
		if out == "" {
			// Fallback to default or prompt
			if in.Default != "" {
				r.cache[id] = in.Default
				return in.Default, nil
			}
			lbl := in.Description
			if strings.TrimSpace(lbl) == "" {
				lbl = fmt.Sprintf("Enter %s", in.ID)
			}
			val, err := promptString(lbl, "", false)
			if err != nil {
				return "", err
			}
			r.cache[id] = val
			return val, nil
		}
		r.cache[id] = out
		return out, nil

	default:
		// Unknown type → prompt
		val, err := promptString(fmt.Sprintf("Enter %s", in.ID), in.Default, false)
		if err != nil {
			return "", err
		}
		r.cache[id] = val
		return val, nil
	}
}

// --- tiny prompt helpers (not fullscreen) ---

// bellFilter strips ASCII BEL (\a) and implements io.WriteCloser.
// Close is a no-op so we never close stdout/stderr.
type bellFilter struct{ w io.Writer }

func (b bellFilter) Write(p []byte) (int, error) {
	p = bytes.ReplaceAll(p, []byte{'\a'}, nil)
	return b.w.Write(p)
}

func (b bellFilter) Close() error { return nil }

func promptString(label, def string, password bool) (string, error) {
	p := promptui.Prompt{
		Label:   label,
		Default: def,
		Stdout:  bellFilter{os.Stdout},
	}
	if password {
		p.Mask = '*'
	}
	return p.Run()
}

func promptSelect(label string, options []string, def string) (string, error) {
	idx := 0
	if def != "" {
		for i, o := range options {
			if o == def {
				idx = i
				break
			}
		}
	}
	s := promptui.Select{
		Label:     label,
		Items:     options,
		CursorPos: idx,
		Size:      minInt(8, maxInt(3, len(options))), // small window; never fullscreen
		Stdout:    bellFilter{os.Stdout},
	}
	_, val, err := s.Run()
	return val, err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func simpleLinePrompt(label, def string) (string, error) {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	br := bufio.NewReader(os.Stdin)
	s, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	s = strings.TrimRight(s, "\r\n")
	if s == "" && def != "" {
		return def, nil
	}
	return s, nil
}

func runInputShell(script string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	exe, args := defaultShell()
	cmd := exec.Command(exe, append(args, script)...)
	// Inherit env and CWD; capture stdout
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// ----------------- existing helpers -----------------

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
