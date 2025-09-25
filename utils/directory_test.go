package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---- FileExists / DirExists -------------------------------------------------

func TestFileAndDirExists(t *testing.T) {
	tmp := t.TempDir()

	// Make a dir and a file
	subdir := filepath.Join(tmp, "a_dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	f := filepath.Join(tmp, "a_file.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !DirExists(subdir) {
		t.Fatalf("DirExists(%q) = false, want true", subdir)
	}
	if DirExists(f) {
		t.Fatalf("DirExists(%q) = true (is file), want false", f)
	}

	if !FileExists(f) {
		t.Fatalf("FileExists(%q) = false, want true", f)
	}
	if FileExists(subdir) {
		t.Fatalf("FileExists(%q) = true (is dir), want false", subdir)
	}

	// Non-existent
	none := filepath.Join(tmp, "nope")
	if DirExists(none) {
		t.Fatalf("DirExists(%q) = true, want false", none)
	}
	if FileExists(none) {
		t.Fatalf("FileExists(%q) = true, want false", none)
	}
}

// ---- getParentDir -----------------------------------------------------------

func TestGetParentDir(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Happy path
	p, err := getParentDir(child)
	if err != nil {
		t.Fatalf("getParentDir(%q) err: %v", child, err)
	}
	if p != filepath.Dir(child) {
		t.Fatalf("getParentDir(%q) = %q, want %q", child, p, filepath.Dir(child))
	}

	// Error at root
	root := rootPath()
	if _, err := getParentDir(root); err == nil {
		t.Fatalf("getParentDir(%q) expected error at root", root)
	}
}

func rootPath() string {
	if runtime.GOOS == "windows" {
		// Use current drive root, normalize to backslash form.
		wd, _ := os.Getwd()
		vol := filepath.VolumeName(wd) // e.g. "C:"
		if vol == "" {
			return `\`
		}
		return vol + `\`
	}
	return "/"
}

// ---- FindProjectRootFrom ----------------------------------------------------

func TestFindProjectRootFrom_FindsVSCodeDir(t *testing.T) {
	ws := t.TempDir()

	// Create workspace with .vscode at the workspace root
	vscode := filepath.Join(ws, VSCODE_DIR)
	if err := os.MkdirAll(vscode, 0o755); err != nil {
		t.Fatalf("mkdir .vscode: %v", err)
	}

	// Create nested path: ws/sub/child
	child := filepath.Join(ws, "sub", "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	// The implementation uses path.Join (POSIX) internally and returns the root path of the project
	// when it finds a .vscode somewhere above. It expects a POSIX-style path
	// string. Forward slashes work on all OSes in Go, including Windows.
	posixChild := toSlash(child)

	got, err := FindProjectRootFrom(posixChild)
	if err != nil {
		t.Fatalf("FindProjectRootFrom(%q) err: %v", posixChild, err)
	}
	requireSamePath(t, got, ws)
}

func TestFindProjectRootFrom_NoVSCodeDirError(t *testing.T) {
	// Create an isolated tree with no .vscode anywhere up to its own temp root.
	ws := t.TempDir()
	leaf := filepath.Join(ws, "a", "b", "c")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("mkdir leaf: %v", err)
	}
	posixLeaf := toSlash(leaf)

	if _, err := FindProjectRootFrom(posixLeaf); err == nil {
		t.Fatalf("FindProjectRootFrom without .vscode expected error")
	}
}

// ---- FindProjectRoot (uses cwd) --------------------------------------------

func TestFindProjectRoot_UsesCWD(t *testing.T) {
	// Build a ws/.vscode and chdir into a nested subdir.
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, VSCODE_DIR), 0o755); err != nil {
		t.Fatalf("mkdir .vscode: %v", err)
	}
	nested := filepath.Join(ws, "pkg", "lib")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	oldwd, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldwd)
	}()

	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot err: %v", err)
	}
	requireSamePath(t, got, ws)
}

// ---- helpers ----------------------------------------------------------------

func toSlash(p string) string {
	// Convert to POSIX-style (matches utils' use of path.Join/Dir)
	return strings.ReplaceAll(p, `\`, `/`)
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		// Fall back to a cleaned path if symlink resolution fails in CI
		return filepath.Clean(p)
	}
	return r
}

func requireSamePath(t *testing.T, got, want string) {
	t.Helper()
	gr, wr := mustReal(t, got), mustReal(t, want)
	if gr != wr {
		t.Fatalf("paths differ:\n  got:  %q (real %q)\n  want: %q (real %q)", got, gr, want, wr)
	}
}
