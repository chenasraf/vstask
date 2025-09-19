package tasks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chenasraf/vstask/utils"
	"github.com/ktr0731/go-fuzzyfinder"
	json "github.com/neilotoole/jsoncolor"
)

func PromptForTask() (Task, error) {
	taskList, err := GetTasks()
	if err != nil {
		return Task{}, err
	}

	idx, err := fuzzyfinder.Find(
		taskList,
		func(i int) string {
			return taskList[i].Label
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i == -1 {
				return "No task selected"
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)

			if json.IsColorTerminal(os.Stdout) {
				clrs := json.DefaultColors()
				enc.SetColors(clrs)
			}

			enc.SetIndent("", "  ")
			err := enc.Encode(taskList[i])
			if err != nil {
				return "Error displaying task details"
			}
			return buf.String()
		}))

	if err != nil {
		if err == fuzzyfinder.ErrAbort {
			return Task{}, nil
		}
		return Task{}, err
	}

	return taskList[idx], nil
}

// GetInputs loads .vscode/tasks.json from the nearest project root and returns the "inputs" array.
// If the file exists but has no inputs, it returns an empty slice (not nil).
func GetInputs() ([]Input, error) {
	root, err := utils.FindProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("find project root: %w", err)
	}

	p := filepath.Join(root, ".vscode", "tasks.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read tasks.json: %w", err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse tasks.json: %w", err)
	}

	if f.Inputs == nil {
		return []Input{}, nil
	}
	return f.Inputs, nil
}
