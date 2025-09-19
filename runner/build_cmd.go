package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/chenasraf/vstask/tasks"
)

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

	case "npm":
		npmExe := tasks.ResolvePackageManagerExecutable(cwd, "npm")

		// Support either:
		// - Command/Script = npm subcommand or script name
		// - Command empty with first arg being subcommand/script

		if s := strings.TrimSpace(t.Script); s != "" {
			npmArgs := []string{"run", s}
			if len(t.Args) > 0 {
				npmArgs = append(npmArgs, "--")
				npmArgs = append(npmArgs, t.Args...)
			}
			cmd := exec.Command(npmExe, npmArgs...)
			cmd.Dir = cwd
			cmd.Env = env
			return cmd, cleanup, nil
		}

		cmdName := strings.TrimSpace(t.Command)
		args := append([]string(nil), t.Args...)

		if cmdName == "" {
			if len(args) == 0 {
				return nil, cleanup, errors.New("npm task missing command/script")
			}
			cmdName, args = args[0], args[1:]
		}

		var npmArgs []string
		switch cmdName {
		case "run", "run-script":
			if len(args) == 0 {
				return nil, cleanup, errors.New("npm run requires a script name")
			}
			npmArgs = append(npmArgs, "run", args[0])
			if len(args) > 1 {
				// Pass remaining as script args after `--`
				npmArgs = append(npmArgs, "--")
				npmArgs = append(npmArgs, args[1:]...)
			}
		default:
			if isNpmBuiltin(cmdName) {
				// Native npm subcommand, e.g. `npm ci`, `npm install`, etc.
				npmArgs = append(npmArgs, cmdName)
				npmArgs = append(npmArgs, args...)
			} else {
				// Treat as package script: `npm run <script> -- <args...>`
				npmArgs = append(npmArgs, "run", cmdName)
				if len(args) > 0 {
					npmArgs = append(npmArgs, "--")
					npmArgs = append(npmArgs, args...)
				}
			}
		}

		cmd := exec.Command(npmExe, npmArgs...)
		cmd.Dir = cwd
		cmd.Env = env
		return cmd, cleanup, nil

	default:
		return nil, cleanup, fmt.Errorf("unsupported task type: %q", t.Type)
	}
}
