package utils

import (
	"errors"
	"os"
	"path"
)

const (
	VSCODE_DIR = ".vscode"
	TASKS_JSON = "tasks.json"
)

func FindProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if cwd == "/" || cwd == "\\" || len(cwd) <= 2 {
		return "", errors.New("no project root found")
	}
	return FindProjectRootFrom(cwd)
}

func FindProjectRootFrom(p string) (string, error) {
	vscodePath := path.Join(p, VSCODE_DIR)
	if DirExists(vscodePath) {
		return ".", nil
	}
	parent, err := getParentDir(p)
	if err != nil {
		return "", err
	}
	return FindProjectRootFrom(parent)
}

func getParentDir(p string) (string, error) {
	if p == "/" || p == "\\" || len(p) <= 2 {
		return "", errors.New("no parent directory")
	}
	return path.Dir(p), nil
}

// FileExists reports whether path exists and is a regular file.
func FileExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
		// return !errors.Is(err, fs.ErrNotExist)
	}
	return info.Mode().IsRegular()
}

// DirExists reports whether path exists and is a directory.
func DirExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
		// return !errors.Is(err, fs.ErrNotExist)
	}
	return info.IsDir()
}

// PathExists reports whether any filesystem object exists at path (file, dir, symlink target, etc.).
func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
	// return err == nil || !errors.Is(err, fs.ErrNotExist)
}
