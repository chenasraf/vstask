package tasks

import (
	"encoding/json"
	"fmt"
)

// File is the root of .vscode/tasks.json
type File struct {
	Version string `json:"version,omitempty"`
	Tasks   []Task `json:"tasks,omitempty"`
	Inputs  []any  `json:"inputs,omitempty"` // keep flexible; inputs can vary
}

// Task represents a single VS Code task (2.0.0 schema).
type Task struct {
	// Required
	Label string `json:"label,omitempty"`
	Type  string `json:"type,omitempty"` // e.g. "shell" | "process" | extension task type

	// Command & args
	Command      string        `json:"command,omitempty"`
	Args         []string      `json:"args,omitempty"`
	Windows      *PlatformTask `json:"windows,omitempty"`
	Osx          *PlatformTask `json:"osx,omitempty"`
	Linux        *PlatformTask `json:"linux,omitempty"`
	Options      *Options      `json:"options,omitempty"`
	Presentation *Presentation `json:"presentation,omitempty"` // aka presentationOptions in older docs
	RunOptions   *RunOptions   `json:"runOptions,omitempty"`

	// Dependencies & grouping
	DependsOn    json.RawMessage `json:"dependsOn,omitempty"`    // string | string[] | { tasks: string[] } via extension
	DependsOrder string          `json:"dependsOrder,omitempty"` // "sequence" | "parallel"
	Group        *Group          `json:"group,omitempty"`        // "build" | "test" | { "kind": "...", "isDefault": bool }

	// Problem matchers (string | string[] | object | object[])
	ProblemMatcher json.RawMessage `json:"problemMatcher,omitempty"`

	// Misc
	Detail string `json:"detail,omitempty"` // shown in the UI
}

// PlatformTask lets you override per-OS parts of the task.
// VS Code allows most of the same fields as the base task here.
type PlatformTask struct {
	Command      string        `json:"command,omitempty"`
	Args         []string      `json:"args,omitempty"`
	Options      *Options      `json:"options,omitempty"`
	Presentation *Presentation `json:"presentation,omitempty"`
}

// Options corresponds to "options" in tasks.json.
type Options struct {
	Cwd   string            `json:"cwd,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Shell *ShellOptions     `json:"shell,omitempty"`
	// Windows/Osx/Linux sub-options also exist - TODO add if needed
}

// ShellOptions controls the shell used by "type": "shell" tasks.
type ShellOptions struct {
	Executable string   `json:"executable,omitempty"`
	Args       []string `json:"args,omitempty"`
	// Quote settings exist too; add if needed (e.g. "quoting": "escape")
}

// Presentation controls terminal/UI behavior.
type Presentation struct {
	Reveal           string `json:"reveal,omitempty"` // "always" | "silent" | "never"
	Panel            string `json:"panel,omitempty"`  // "shared" | "dedicated" | "new"
	Focus            bool   `json:"focus,omitempty"`
	Echo             bool   `json:"echo,omitempty"`
	ShowReuseMessage bool   `json:"showReuseMessage,omitempty"`
	Clear            bool   `json:"clear,omitempty"`
	// "RevealProblems": "onProblem"|"onProblemDependingOnSeverity" may exist in newer versions
}

// Group can be a simple string ("build"/"test") or an object.
type Group struct {
	// If Kind is empty but you need simple groups, you can instead store "build" or "test"
	// directly in JSON by using Raw below.
	Kind      string `json:"kind,omitempty"`      // e.g. "build", "test"
	IsDefault bool   `json:"isDefault,omitempty"` // marks default task for the group
}

// UnmarshalJSON supports both the string and object forms.
func (g *Group) UnmarshalJSON(b []byte) error {
	// Try simple string: "build"
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*g = Group{Kind: s}
		return nil
	}

	// Try object: { "kind": "...", "isDefault": true }
	type alias Group
	var obj alias
	if err := json.Unmarshal(b, &obj); err == nil {
		*g = Group(obj)
		return nil
	}

	return fmt.Errorf("group: invalid value %s", string(b))
}

// Optional: Marshal as string when IsDefault is false, otherwise as object.
func (g Group) MarshalJSON() ([]byte, error) {
	if !g.IsDefault {
		return json.Marshal(g.Kind)
	}
	type alias Group
	return json.Marshal(alias(g))
}

// RunOptions adds scheduling behavior (VS Code 1.59+).
type RunOptions struct {
	RunOn           string `json:"runOn,omitempty"`           // "default" | "folderOpen"
	ReevaluateOnRun bool   `json:"reevaluateOnRun,omitempty"` // true: re-resolve variables each run
	InstanceLimit   int    `json:"instanceLimit,omitempty"`   // max parallel instances
}
