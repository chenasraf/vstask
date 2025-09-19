package runner

// put this near the top of the file or below imports
func isNpmBuiltin(cmd string) bool {
	switch cmd {
	case "install", "ci", "publish", "pack", "update", "outdated",
		"rebuild", "version", "login", "logout", "whoami",
		"init", "create", "audit", "prune", "cache", "config",
		"root", "help", "search", "exec", "add", "remove",
		"link", "unlink", "run", "run-script":
		return true
	default:
		return false
	}
}
