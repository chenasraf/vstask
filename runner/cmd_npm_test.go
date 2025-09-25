package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chenasraf/vstask/tasks"
)

// --- helpers ---

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func isolatePMDetectionToDefault(t *testing.T) {
	t.Helper()
	// Point HOME/XDG/APPDATA somewhere empty so no user settings are found
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg-empty"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata-empty"))
	// No .vscode/settings.json in the workspace we pass (we'll use t.TempDir()).
}

// --- npm buildCmd behavior ---

func TestBuildCmd_Npm_BuiltinSubcommand(t *testing.T) {
	isolatePMDetectionToDefault(t)

	// npm ci --prefer-offline
	tk := tasks.Task{
		Type:    "npm",
		Command: "ci",
		Args:    []string{"--prefer-offline"},
	}
	cmd, _, err := buildCmd(tk, t.TempDir(), os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}

	// Exec is first arg; path resolution happens at Start(), so check Args[0]
	if got, want := filepath.Base(cmd.Args[0]), "npm"; got != want {
		t.Fatalf("exe=%q, want %q", got, want)
	}
	if len(cmd.Args) < 3 || cmd.Args[1] != "ci" || cmd.Args[2] != "--prefer-offline" {
		t.Fatalf("argv=%v, want [npm ci --prefer-offline...]", cmd.Args)
	}
}

func TestBuildCmd_Npm_UsesScriptField(t *testing.T) {
	isolatePMDetectionToDefault(t)

	ws := t.TempDir()
	tk := tasks.Task{
		Type:   "npm",
		Script: "build",
		Args:   []string{"--flag"},
	}
	cmd, _, err := buildCmd(tk, ws, os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	got := append([]string{filepath.Base(cmd.Args[0])}, cmd.Args[1:]...)
	want := []string{"npm", "run", "build", "--", "--flag"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv=%v, want %v", got, want)
	}
}

func TestBuildCmd_Npm_Script_DefaultsToRun(t *testing.T) {
	isolatePMDetectionToDefault(t)

	// npm run lint -- --fix
	tk := tasks.Task{
		Type:    "npm",
		Command: "lint", // not a builtin -> treated as script
		Args:    []string{"--fix"},
	}
	cmd, _, err := buildCmd(tk, t.TempDir(), os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	if got, want := filepath.Base(cmd.Args[0]), "npm"; got != want {
		t.Fatalf("exe=%q, want %q", got, want)
	}
	wantSeq := []string{"npm", "run", "lint", "--", "--fix"}
	gotSeq := append([]string{filepath.Base(cmd.Args[0])}, cmd.Args[1:]...)
	if strings.Join(gotSeq, " ") != strings.Join(wantSeq, " ") {
		t.Fatalf("argv=%v, want %v", gotSeq, wantSeq)
	}
}

func TestBuildCmd_Npm_RunExplicit(t *testing.T) {
	isolatePMDetectionToDefault(t)

	// npm run build -- --flag
	tk := tasks.Task{
		Type:    "npm",
		Command: "run",
		Args:    []string{"build", "--flag"},
	}
	cmd, _, err := buildCmd(tk, t.TempDir(), os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	wantSeq := []string{"npm", "run", "build", "--", "--flag"}
	gotSeq := append([]string{filepath.Base(cmd.Args[0])}, cmd.Args[1:]...)
	if strings.Join(gotSeq, " ") != strings.Join(wantSeq, " ") {
		t.Fatalf("argv=%v, want %v", gotSeq, wantSeq)
	}
}

func TestBuildCmd_Npm_EmptyCommandUsesArgs0(t *testing.T) {
	isolatePMDetectionToDefault(t)

	// npm ci  (command is empty; first arg is used)
	tk := tasks.Task{
		Type: "npm",
		Args: []string{"ci"},
	}
	cmd, _, err := buildCmd(tk, t.TempDir(), os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	gotSeq := append([]string{filepath.Base(cmd.Args[0])}, cmd.Args[1:]...)
	wantSeq := []string{"npm", "ci"}
	if strings.Join(gotSeq, " ") != strings.Join(wantSeq, " ") {
		t.Fatalf("argv=%v, want %v", gotSeq, wantSeq)
	}
}

// --- npm.packageManager setting resolution ---

func TestResolvePM_FromWorkspaceSettings(t *testing.T) {
	ws := t.TempDir()
	// .vscode/settings.json with pnpm
	writeFile(t, filepath.Join(ws, ".vscode", "settings.json"), `{
		// comments allowed
		"npm.packageManager": "pnpm"
	}`)
	// type npm + "help" → should pick pnpm
	tk := tasks.Task{Type: "npm", Command: "help"}
	cmd, _, err := buildCmd(tk, ws, os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	if got, want := filepath.Base(cmd.Args[0]), "pnpm"; got != want {
		t.Fatalf("exe=%q, want %q (workspace override)", got, want)
	}
}

func TestResolvePM_FromUserSettings(t *testing.T) {
	// Create a fake user settings dir and point env vars to it.
	tmp := t.TempDir()
	var userSettings string

	switch runtime.GOOS {
	case "darwin":
		home := tmp
		_ = os.Setenv("HOME", home)
		userSettings = filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	case "linux":
		xdg := filepath.Join(tmp, "xdg")
		_ = os.Setenv("XDG_CONFIG_HOME", xdg)
		userSettings = filepath.Join(xdg, "Code", "User", "settings.json")
	case "windows":
		// VS Code uses %APPDATA%\Code\User\settings.json
		appdata := filepath.Join(tmp, "AppData", "Roaming")
		_ = os.Setenv("APPDATA", appdata)
		userSettings = filepath.Join(appdata, "Code", "User", "settings.json")
	default:
		t.Skip("unknown OS layout")
	}

	writeFile(t, userSettings, `{"npm.packageManager":"yarn"}`)

	ws := t.TempDir() // no workspace settings
	tk := tasks.Task{Type: "npm", Command: "help"}
	cmd, _, err := buildCmd(tk, ws, os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	if got, want := filepath.Base(cmd.Args[0]), "yarn"; got != want {
		t.Fatalf("exe=%q, want %q (user override)", got, want)
	}
}

// --- builtin detector ---

func TestIsNpmBuiltin(t *testing.T) {
	// This test is in the same package, so we can reach unexported isNpmBuiltin if present.
	tests := map[string]bool{
		"install": true,
		"ci":      true,
		"run":     true,
		"help":    true,
		"exec":    true,
		"add":     true,
		"remove":  true,
		"foo":     false, // Non-builtin => treated as script
	}
	for k, want := range tests {
		if got := isNpmBuiltin(k); got != want {
			t.Fatalf("isNpmBuiltin(%q)=%v, want %v", k, got, want)
		}
	}
}

// --- sanity: buildCmd uses chosen env/cwd unchanged ---

func TestBuildCmd_Npm_PreservesEnvAndCwd(t *testing.T) {
	isolatePMDetectionToDefault(t)

	ws := t.TempDir()
	tk := tasks.Task{Type: "npm", Command: "help"}

	env := append(os.Environ(), "FOO=bar")
	cmd, _, err := buildCmd(tk, ws, env)
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	if cmd.Dir != ws {
		t.Fatalf("cwd=%q, want %q", cmd.Dir, ws)
	}
	m := envMap(cmd.Env)
	if m["FOO"] != "bar" {
		t.Fatalf("env FOO missing, got=%q", m["FOO"])
	}
}

// Guard: ensure rebuildWithSh only touches argv[0]
func TestRebuildWithSh_SwapsOnlyExe(t *testing.T) {
	c := exec.Command("/bin/bash", "-lc", "echo ok")
	c2 := rebuildWithSh(c)
	if c2 == nil {
		t.Fatal("rebuildWithSh returned nil")
	}
	if base := filepath.Base(c2.Args[0]); base != "sh" {
		t.Fatalf("exe=%q, want sh", c2.Args[0])
	}
	if strings.Join(c2.Args[1:], " ") != strings.Join(c.Args[1:], " ") {
		t.Fatalf("args changed: %v -> %v", c.Args, c2.Args)
	}
	if c2.Dir != c.Dir {
		t.Fatalf("Dir changed")
	}
	if strings.Join(c2.Env, "\x00") != strings.Join(c.Env, "\x00") {
		t.Fatalf("Env changed")
	}
}
