package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chenasraf/vstask/utils"
)

// -----------------------------
// VS Code settings model
// -----------------------------
type VSCodeSettings struct {
	// Matches the VS Code setting: "npm.packageManager"
	// Valid values: "npm", "yarn", "pnpm", "bun"
	NPMPackageManager string `json:"npm.packageManager"`
}

// -----------------------------
// Public entry point
// -----------------------------

// detectPackageManagerFromSettings tries to read the package manager preference
// from (1) workspace .vscode/settings.json, then (2) user settings.json.
// If found and valid, returns (exe, true). Otherwise ("", false).
func detectPackageManagerFromSettings(cwd string) (string, bool) {
	// 1) Workspace settings
	if exe, ok := readWorkspacePackageManager(cwd); ok {
		return exe, true
	}
	// 2) User settings
	if exe, ok := readUserPackageManager(); ok {
		return exe, true
	}
	return "", false
}

// -----------------------------
// Workspace settings
// -----------------------------

func readWorkspacePackageManager(cwd string) (string, bool) {
	path := filepath.Join(cwd, ".vscode", "settings.json")
	return readPackageManagerFromFile(path)
}

// -----------------------------
// User settings (Code / Insiders / VSCodium)
// -----------------------------

func readUserPackageManager() (string, bool) {
	candidates := userSettingsCandidates()
	for _, p := range candidates {
		if exe, ok := readPackageManagerFromFile(p); ok {
			return exe, true
		}
	}
	return "", false
}

func userSettingsCandidates() []string {
	var dirs []string

	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		appSupport := filepath.Join(home, "Library", "Application Support")
		if home != "" {
			dirs = append(dirs,
				filepath.Join(appSupport, "Code", "User", "settings.json"),
				filepath.Join(appSupport, "Code - Insiders", "User", "settings.json"),
				filepath.Join(appSupport, "VSCodium", "User", "settings.json"),
			)
		}
	case "linux":
		home, _ := os.UserHomeDir()
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg != "" {
			dirs = append(dirs,
				filepath.Join(xdg, "Code", "User", "settings.json"),
				filepath.Join(xdg, "Code - Insiders", "User", "settings.json"),
				filepath.Join(xdg, "VSCodium", "User", "settings.json"),
			)
		}
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, ".config", "Code", "User", "settings.json"),
				filepath.Join(home, ".config", "Code - Insiders", "User", "settings.json"),
				filepath.Join(home, ".config", "VSCodium", "User", "settings.json"),
			)
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dirs = append(dirs,
				filepath.Join(appData, "Code", "User", "settings.json"),
				filepath.Join(appData, "Code - Insiders", "User", "settings.json"),
				filepath.Join(appData, "VSCodium", "User", "settings.json"),
			)
		}
	default:
		// Fallback: try common Linux layout
		home, _ := os.UserHomeDir()
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".config", "Code", "User", "settings.json"))
		}
	}

	return dirs
}

// -----------------------------
// File loader
// -----------------------------

func readPackageManagerFromFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	clean := utils.ConvertJsoncToJson(b)

	var s VSCodeSettings
	if err := json.Unmarshal([]byte(clean), &s); err != nil {
		return "", false
	}

	exe, ok := normalizePM(s.NPMPackageManager)
	if !ok {
		return "", false
	}
	return exe, true
}

func normalizePM(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "npm":
		return "npm", true
	case "yarn":
		return "yarn", true
	case "pnpm":
		return "pnpm", true
	case "bun":
		return "bun", true
	default:
		return "", false
	}
}

func ResolvePackageManagerExecutable(cwd string, defaultExe string) string {
	if exe, ok := detectPackageManagerFromSettings(cwd); ok {
		return exe
	}
	if defaultExe == "" {
		defaultExe = "npm"
	}
	return defaultExe
}
