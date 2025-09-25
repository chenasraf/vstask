package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chenasraf/vstask/tasks"
)

func TestBuildVSCodeVarMapWithCWD(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "ws")
	_ = os.MkdirAll(workspace, 0o755)

	vars := buildVSCodeVarMapWithCWD(workspace, filepath.Join(workspace, "sub"))
	if got, want := vars["workspaceFolder"], workspace; got != want {
		t.Fatalf("workspaceFolder = %q, want %q", got, want)
	}
	if got, want := vars["workspaceFolderBasename"], filepath.Base(workspace); got != want {
		t.Fatalf("workspaceFolderBasename = %q, want %q", got, want)
	}
	if got, want := vars["cwd"], filepath.Join(workspace, "sub"); got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
	if sep := vars["pathSeparator"]; sep == "" {
		t.Fatalf("pathSeparator empty")
	}
	if slash := vars["/"]; slash != string(os.PathSeparator) {
		t.Fatalf("${/} = %q, want %q", slash, string(os.PathSeparator))
	}
}

func TestSubstituteVarsSimple(t *testing.T) {
	vars := map[string]string{
		"userHome":        "/home/me",
		"workspaceFolder": "/w/s",
		"cwd":             "/w/s/app",
	}
	in := "cd ${cwd} && echo ${workspaceFolder} ${userHome}"
	out := substituteVars(in, vars)
	if want := "cd /w/s/app && echo /w/s /home/me"; out != want {
		t.Fatalf("substituteVars out=%q, want %q", out, want)
	}
}

func TestCWDResolution_RelativeFromOptions(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "ws")
	rel := "sub/dir"
	wantCWD := filepath.Join(workspace, rel)
	_ = os.MkdirAll(wantCWD, 0o755)

	tk := tasks.Task{
		Options: &tasks.Options{Cwd: rel},
	}

	// prelim vars (use current process cwd)
	pre := buildVSCodeVarMapWithCWD(workspace, mustGetwd())

	// apply same logic as runSingleTask top
	cwd := workspace
	if tk.Options != nil && tk.Options.Cwd != "" {
		cwdr := substituteVars(tk.Options.Cwd, pre)
		if filepath.IsAbs(cwdr) {
			cwd = cwdr
		} else {
			cwd = filepath.Join(workspace, cwdr)
		}
	}
	if cwd != wantCWD {
		t.Fatalf("resolved cwd=%q, want %q", cwd, wantCWD)
	}
}

func TestBuildCommandLine_Posix_NoArgs_PassesVerbatim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting test")
	}
	cmd := "echo $(printf foo) | tr o O"
	line := buildCommandLine(cmd, nil)
	if line != cmd {
		t.Fatalf("line=%q, want verbatim %q", line, cmd)
	}
}

func TestBuildCommandLine_Posix_QuotesOnlyArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting test")
	}
	cmd := "printf"
	args := []string{"Hello World", "$HOME", `a"b`}
	line := buildCommandLine(cmd, args)
	// Command must be verbatim, args quoted.
	if !strings.HasPrefix(line, "printf ") {
		t.Fatalf("line prefix=%q", line)
	}
	if !strings.Contains(line, `"Hello World"`) {
		t.Fatalf("missing quoted arg for space: %q", line)
	}
	if !strings.Contains(line, `"$HOME"`) {
		t.Fatalf("missing quoted arg with $: %q", line)
	}
	if !strings.Contains(line, `"a\"b"`) && !strings.Contains(line, `"a\\\"b"`) {
		t.Fatalf("missing escaped quote in arg: %q", line)
	}
}

func TestBuildCmd_Shell_DefaultKeepsDashC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell semantics test on POSIX")
	}
	tk := tasks.Task{
		Type:    "shell",
		Command: "echo ok",
		Options: &tasks.Options{
			Shell: &tasks.ShellOptions{Executable: "/bin/sh"},
		},
	}
	cmd, _, err := buildCmd(tk, "/", os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	argv := cmd.Args
	hasDashC := slices.Contains(argv, "-c")
	if !hasDashC {
		t.Fatalf("expected -c in shell args, got %v", argv)
	}
}

func TestBuildCmd_Shell_CustomArgsOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell semantics test on POSIX")
	}
	tk := tasks.Task{
		Type:    "shell",
		Command: "echo ok",
		Options: &tasks.Options{
			Shell: &tasks.ShellOptions{
				Executable: "/bin/sh",
				Args:       []string{"-lc"}, // explicit, non-empty
			},
		},
	}
	cmd, _, err := buildCmd(tk, "/", os.Environ())
	if err != nil {
		t.Fatalf("buildCmd err: %v", err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, " -lc ") && !strings.HasSuffix(joined, " -lc") {
		t.Fatalf("expected custom shell args in %v", cmd.Args)
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"A=1", "B=2"}
	extra := map[string]string{"B": "3", "C": "4"}
	out := mergeEnv(base, extra)
	got := envToMap(out)
	if got["A"] != "1" || got["B"] != "3" || got["C"] != "4" {
		t.Fatalf("mergeEnv bad result: %#v", got)
	}
}

func envToMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// ------------- Light integration tests (POSIX) -------------

func TestRunSingleTask_ShellEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell integration")
	}

	workspace := t.TempDir()
	// Use printf to avoid echo builtin inconsistencies in some shells
	tk := tasks.Task{
		Type:    "shell",
		Command: "printf Hello",
	}
	var buf bytes.Buffer

	// Build cmd and run quickly (simulate most of runSingleTask without signals)
	pre := buildVSCodeVarMapWithCWD(workspace, mustGetwd())
	cwd := workspace
	vars := buildVSCodeVarMapWithCWD(workspace, cwd)
	tk.Command = substituteVars(tk.Command, vars)

	cmd, cleanup, err := buildCmd(tk, cwd, os.Environ())
	if err != nil {
		t.Fatalf("buildCmd: %v", err)
	}
	defer cleanup()
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v (%s)", err, buf.String())
	}
	if got := buf.String(); got != "Hello" {
		t.Fatalf("stdout=%q, want Hello", got)
	}
	// ensure pre-vars used (to avoid unused var lints)
	_ = pre
}

func TestStartAndWaitReady_Background_UnblocksQuickly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell integration")
	}
	workspace := t.TempDir()

	// A background "watcher" that prints a $tsc-watch readiness line, then sleeps briefly
	watcher := tasks.Task{
		Type:         "shell",
		IsBackground: true,
		ProblemMatcher: &tasks.ProblemMatcher{
			Elems: []json.RawMessage{json.RawMessage(`"$tsc-watch"`)},
		},
		// readiness line then linger a bit
		Command: `printf "Starting compilation in watch mode...\n"; sleep 0.5`,
	}

	// Build command for the watcher
	cmd, cleanup, err := buildCmd(watcher, workspace, os.Environ())
	if err != nil {
		t.Fatalf("buildCmd watcher: %v", err)
	}
	defer cleanup()

	// Compile the background matcher via extractBgMatcher (uses ProblemMatcher.FirstBackground)
	bg := extractBgMatcher(watcher)
	if bg == nil {
		t.Fatal("expected bg matcher, got nil")
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := startAndWaitReady(ctx, &execCmdShim{Cmd: cmd}, false, bg, true); err != nil {
		t.Fatalf("startAndWaitReady err: %v", err)
	}
	elapsed := time.Since(start)

	// Should return quickly (almost immediately after printing the line)
	if elapsed > time.Second {
		t.Fatalf("readiness gating took too long: %v", elapsed)
	}
}

// ------------- Windows equivalents (optional stubs) -------------

func TestDefaultShell_WindowsOrPosix(t *testing.T) {
	exe, args := defaultShell()
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(strings.ToLower(exe), "cmd.exe") {
			t.Fatalf("windows shell exe=%q", exe)
		}
		found := slices.Contains(args, "/C")
		if !found {
			t.Fatalf("expected /C in windows shell args: %v", args)
		}
	} else {
		if exe != "/bin/sh" {
			// Allow different shells if ever customized, but default is /bin/sh
			t.Logf("POSIX shell exe=%q", exe)
		}
		found := slices.Contains(args, "-c")
		if !found {
			t.Fatalf("expected -c in POSIX shell args: %v", args)
		}
	}
}

// (Optional) A very fast run to ensure buildCmd "process" mode is OK
func TestBuildCmd_ProcessOK(t *testing.T) {
	var cmdName string
	var cmdArgs []string
	if runtime.GOOS == "windows" {
		cmdName = "cmd"
		cmdArgs = []string{"/C", "exit", "0"}
	} else {
		cmdName = "true"
	}
	tk := tasks.Task{
		Type:    "process",
		Command: cmdName,
		Args:    cmdArgs,
	}
	cmd, _, err := buildCmd(tk, "/", os.Environ())
	if err != nil {
		t.Fatalf("buildCmd process err: %v", err)
	}
	c := *cmd
	c.Stdout = &bytes.Buffer{}
	c.Stderr = &bytes.Buffer{}
	// Give it a small timeout to avoid hangs in CI
	done := make(chan error, 1)
	go func() { done <- c.Run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process task failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process task timed out")
	}
}
