package tasks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// File is the root of .vscode/tasks.json
type File struct {
	Version string  `json:"version,omitempty"`
	Tasks   []Task  `json:"tasks,omitempty"`
	Inputs  []Input `json:"inputs,omitempty"` // VS Code "inputs" array
}

// -------------------------
// Input (VS Code "inputs")
// -------------------------
//
// Matches VS Code's input types:
// - promptString: { "id", "type":"promptString", "description"?, "default"?, "password"? }
// - pickString:   { "id", "type":"pickString",  "description"?, "options":[...], "default"? }
// - command:      { "id", "type":"command",     "command":"...", "args"?: any, "description"?, "default"? }
//
// Note: We keep a superset struct; unused fields simply stay zero.
type Input struct {
	ID          string   `json:"id,omitempty"`
	Type        string   `json:"type,omitempty"`        // "promptString" | "pickString" | "command"
	Description string   `json:"description,omitempty"` // shown to the user
	Default     string   `json:"default,omitempty"`     // default value if user just presses Enter
	Password    bool     `json:"password,omitempty"`    // promptString only
	Options     []string `json:"options,omitempty"`     // pickString only

	// Command input
	Command string          `json:"command,omitempty"` // command to run; we use its stdout as value
	Args    json.RawMessage `json:"args,omitempty"`    // optional args payload (not used by runner yet)
}

// DescriptionOrFallback returns a non-empty label for prompting.
func (in *Input) DescriptionOrFallback() string {
	if d := in.Description; d != "" {
		return d
	}
	if in.ID != "" {
		return "Select " + in.ID
	}
	return "Select an option"
}

// Task represents a single VS Code task (2.0.0 schema).
type Task struct {
	// Required
	Label string `json:"label,omitempty"`
	Type  string `json:"type,omitempty"` // e.g. "shell" | "process" | extension task type

	// Command & args
	Command      string        `json:"command,omitempty"`
	Script       string        `json:"script,omitempty"`
	Args         []string      `json:"args,omitempty"`
	Windows      *PlatformTask `json:"windows,omitempty"`
	Osx          *PlatformTask `json:"osx,omitempty"`
	Linux        *PlatformTask `json:"linux,omitempty"`
	Options      *Options      `json:"options,omitempty"`
	Presentation *Presentation `json:"presentation,omitempty"` // aka presentationOptions in older docs
	RunOptions   *RunOptions   `json:"runOptions,omitempty"`
	IsBackground bool          `json:"isBackground,omitempty"`

	// Dependencies & grouping
	DependsOn    *DependsOn `json:"dependsOn,omitempty"`    // string | string[] | { tasks: string[] }
	DependsOrder string     `json:"dependsOrder,omitempty"` // "sequence" | "parallel"
	Group        *Group     `json:"group,omitempty"`        // "build" | "test" | { "kind": "...", "isDefault": bool }

	// Problem matchers (string | string[] | object | object[])
	ProblemMatcher *ProblemMatcher `json:"problemMatcher,omitempty"`

	// Misc
	Detail string `json:"detail,omitempty"` // shown in the UI
}

// PlatformTask allows overriding per-OS parts of the task.
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

// -------------------------
// Group (string | object)
// -------------------------

type Group struct {
	Kind      string `json:"kind,omitempty"`      // e.g. "build", "test"
	IsDefault bool   `json:"isDefault,omitempty"` // marks default task for the group
}

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

func (g Group) MarshalJSON() ([]byte, error) {
	if !g.IsDefault {
		return json.Marshal(g.Kind)
	}
	type alias Group
	return json.Marshal(alias(g))
}

// -----------------------------------------
// DependsOn (string | string[] | {tasks})
// -----------------------------------------

type DependsOn struct {
	Tasks []string
}

func (d *DependsOn) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*d = DependsOn{}
		return nil
	}
	// string
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if s != "" {
			d.Tasks = []string{s}
		}
		return nil
	}
	// []string
	var ss []string
	if err := json.Unmarshal(b, &ss); err == nil {
		d.Tasks = ss
		return nil
	}
	// { "tasks": []string }
	var obj struct {
		Tasks []string `json:"tasks"`
	}
	if err := json.Unmarshal(b, &obj); err == nil && obj.Tasks != nil {
		d.Tasks = obj.Tasks
		return nil
	}
	return fmt.Errorf("dependsOn: invalid value %s", string(b))
}

func (d DependsOn) MarshalJSON() ([]byte, error) {
	switch len(d.Tasks) {
	case 0:
		return []byte("null"), nil
	case 1:
		return json.Marshal(d.Tasks[0])
	default:
		return json.Marshal(d.Tasks)
	}
}

// -------------------------------------------------------
// ProblemMatcher (string | string[] | object | object[])
// -------------------------------------------------------

// ProblemMatcher holds one or more problem matcher entries, each preserved as raw JSON.
// Use .Strings() to extract only string matchers, or .Objects() to get raw object entries.
type ProblemMatcher struct {
	Elems []json.RawMessage
}

func (pm *ProblemMatcher) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*pm = ProblemMatcher{}
		return nil
	}

	// Try as array (strings or objects)
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err == nil {
		pm.Elems = arr
		return nil
	}

	// Single string or single object
	var one json.RawMessage
	if err := json.Unmarshal(b, &one); err == nil {
		pm.Elems = []json.RawMessage{one}
		return nil
	}

	return fmt.Errorf("problemMatcher: invalid value %s", string(b))
}

func (pm ProblemMatcher) MarshalJSON() ([]byte, error) {
	switch len(pm.Elems) {
	case 0:
		return []byte("null"), nil
	case 1:
		return json.Marshal(pm.Elems[0])
	default:
		return json.Marshal(pm.Elems)
	}
}

// Convenience helpers (optional)
func (pm ProblemMatcher) Strings() []string {
	out := make([]string, 0, len(pm.Elems))
	for _, e := range pm.Elems {
		var s string
		if err := json.Unmarshal(e, &s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// Raw objects (matcher objects) as raw JSON blobs
func (pm ProblemMatcher) Objects() []json.RawMessage {
	out := make([]json.RawMessage, 0, len(pm.Elems))
	for _, e := range pm.Elems {
		// keep only non-strings (heuristic)
		var s string
		if err := json.Unmarshal(e, &s); err != nil {
			out = append(out, e)
		}
	}
	return out
}

// ----- Typed object form for problem matchers -----

type ProblemMatcherBackground struct {
	ActiveOnStart bool   `json:"activeOnStart,omitempty"`
	BeginsPattern string `json:"beginsPattern,omitempty"`
	EndsPattern   string `json:"endsPattern,omitempty"`
}

type ProblemMatcherPattern struct {
	// We don't need to model full pattern structure for readiness,
	// but keep a placeholder so future extensions can unmarshal cleanly.
	Regexp string `json:"regexp,omitempty"`
}

type ProblemMatcherObject struct {
	Owner        string                    `json:"owner,omitempty"`
	Source       string                    `json:"source,omitempty"`
	FileLocation any                       `json:"fileLocation,omitempty"`
	Pattern      any                       `json:"pattern,omitempty"`    // can be object or array; not used for readiness
	Background   *ProblemMatcherBackground `json:"background,omitempty"` // what we need for readiness gating
	Severity     string                    `json:"severity,omitempty"`
}

// FirstBackground returns the first background config found among object matchers,
// or resolves a known string alias (e.g., "$tsc-watch") into a background config.
// Returns nil if no usable background is present.
func (pm ProblemMatcher) FirstBackground() *ProblemMatcherBackground {
	// 1) Object matchers with background
	for _, raw := range pm.Objects() {
		var obj ProblemMatcherObject
		if err := json.Unmarshal(raw, &obj); err == nil && obj.Background != nil {
			bg := *obj.Background // copy to avoid aliasing
			// Normalize empty strings to zero values
			bg.BeginsPattern = strings.TrimSpace(bg.BeginsPattern)
			bg.EndsPattern = strings.TrimSpace(bg.EndsPattern)
			if bg.ActiveOnStart || bg.BeginsPattern != "" {
				return &bg
			}
		}
	}

	// 2) String aliases → known backgrounds (minimal set; extend as needed)
	for _, s := range pm.Strings() {
		s = strings.TrimSpace(s)
		if bg, ok := builtinBackgroundByAlias[s]; ok {
			// return a copy to avoid external mutation
			cp := bg
			return &cp
		}
	}

	return nil
}

// Minimal built-in alias → background mapping for readiness-only purposes.
var builtinBackgroundByAlias = map[string]ProblemMatcherBackground{
	// TypeScript watch: tsc -w
	// Common lines seen:
	// - "Starting compilation in watch mode..."
	// - "Found 0 errors. Watching for file changes."
	// - "Found X errors. Watching for file changes."
	"$tsc-watch": {
		ActiveOnStart: false,
		BeginsPattern: `(?i)\bwatch(ing)? for file changes\b|^Starting compilation in watch mode`,
		EndsPattern:   ``, // not required for readiness
	},
}

// BgMatcher is used by the runner to hold compiled regexes.
type BgMatcher struct {
	ActiveOnStart bool
	BeginsRx      *regexp.Regexp // optional
	EndsRx        *regexp.Regexp // optional; useful for cycles/logging, not required for readiness
}

// ------------------------
// RunOptions (plain JSON)
// ------------------------

type RunOptions struct {
	RunOn           string `json:"runOn,omitempty"`           // "default" | "folderOpen"
	ReevaluateOnRun bool   `json:"reevaluateOnRun,omitempty"` // true: re-resolve variables each run
	InstanceLimit   int    `json:"instanceLimit,omitempty"`   // max parallel instances
}

func (t Task) IsEmpty() bool {
	return t.Label == "" && t.Command == ""
}
