# vstask

**`vstask`** is a tiny, cross-platform runner for **VS Code `tasks.json`** written in **Go**. It
discovers your project’s `.vscode/tasks.json`, resolves VS Code variables, respects platform
overrides, and executes tasks—**including dependencies in sequence or in parallel**—with proper
signal handling and process-tree cleanup.

![Release](https://img.shields.io/github/v/release/chenasraf/vstask)
![Downloads](https://img.shields.io/github/downloads/chenasraf/vstask/total)
![License](https://img.shields.io/github/license/chenasraf/vstask)

---

## 🚀 Features

- **Zero-config**: auto-discovers `.vscode/tasks.json` from your project tree.
- **VS Code semantics**:
  - Platform overrides (`windows`/`osx`/`linux`)
  - `type: shell` / `type: process`
  - `dependsOn` with **sequence** or **parallel** execution
  - Built-in **variable substitutions** (e.g., `${workspaceFolder}`, `${userHome}`, `${cwd}`, etc.).

- **Robust execution**:
  - Correct shell invocation (`/bin/sh -c` or `cmd.exe /C` by default)
  - Well-formed quoting for args while leaving command strings verbatim (so `$(...)`, pipes, etc.
    work)
  - **Signal trapping** (CTRL-C) and **process-group kill** on Unix; `taskkill /T /F` on Windows
  - Proper working-directory resolution with relative paths

---

## 🎯 Installation

### Download Precompiled Binaries

Grab the latest release for **Linux**, **macOS**, or **Windows**:

- [Releases →](https://github.com/chenasraf/vstask/releases/latest)

### Homebrew (macOS/Linux)

Install from your tap:

```bash
brew install chenasraf/tap/vstask
```

> If you already use your tap for other tools, `brew update` first.

### From Source

```bash
git clone https://github.com/chenasraf/vstask
cd vstask
go build ./...
```

---

## ✨ Getting Started

Place a `tasks.json` under `.vscode/` in your repo. Example:

```jsonc
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "my-command",
      "type": "shell",
      "command": "echo \"it works!\"",
      "dependsOn": ["dep-a", "dep-b"],
      "dependsOrder": "parallel",
      "problemMatcher": []
    },
    {
      "label": "dep-a",
      "type": "shell",
      "command": "echo \"running dep-a\"",
      "problemMatcher": []
    },
    {
      "label": "dep-b",
      "type": "shell",
      "command": "echo \"running dep-b\"",
      "problemMatcher": []
    }
  ]
}
```

### Run a task (CLI)

> If your CLI exposes `run` and `list` commands; adjust if your binary uses a different interface.

```bash
# list tasks and select from the list
vstask

# run one by label
vstask my-command
```

---

## 🛠️ Contributing

I am developing this package on my free time, so any support, whether code, issues, or just stars is
very helpful to sustaining its life. If you are feeling incredibly generous and would like to donate
just a small amount to help sustain this project, I would be very very thankful!

<a href='https://ko-fi.com/casraf' target='_blank'>
<img height='36' style='border:0px;height:36px;' src='https://cdn.ko-fi.com/cdn/kofi1.png?v=3' alt='Buy Me a Coffee at ko-fi.com' />
</a>

I welcome any issues or pull requests on GitHub. If you find a bug, or would like a new feature,
don't hesitate to open an appropriate issue and I will do my best to reply promptly.

---

## 📜 License

`vstask` is licensed under the [CC0-1.0 License](/LICENSE).

---

Happy tasking! 🧰✨
