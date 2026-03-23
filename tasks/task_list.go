package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/chenasraf/vstask/utils"
)

func GetTasks() ([]Task, error) {
	projectRoot, err := utils.FindProjectRoot()
	if err != nil {
		return []Task{}, err
	}

	tasksPath := path.Join(projectRoot, utils.VSCODE_DIR, utils.TASKS_JSON)

	if !utils.FileExists(tasksPath) {
		return []Task{}, errors.New("tasks.json not found")
	}

	return LoadTasksFile(tasksPath)
}

// FindTask looks up a task by name. It first tries an exact match on the label,
// then falls back to case-insensitive substring matching. Returns an error if
// no match is found or if multiple tasks match the query.
func FindTask(taskList []Task, query string) (Task, error) {
	// Exact match
	for _, t := range taskList {
		if t.Label == query {
			return t, nil
		}
	}

	// Case-insensitive partial match
	lower := strings.ToLower(query)
	var matches []Task
	for _, t := range taskList {
		if strings.Contains(strings.ToLower(t.Label), lower) {
			matches = append(matches, t)
		}
	}

	switch len(matches) {
	case 0:
		return Task{}, fmt.Errorf("task not found: %s", query)
	case 1:
		return matches[0], nil
	default:
		labels := make([]string, len(matches))
		for i, m := range matches {
			labels[i] = m.Label
		}
		return Task{}, fmt.Errorf("multiple tasks match '%s':\n  - %s", query, strings.Join(labels, "\n  - "))
	}
}

func LoadTasksFile(tasksPath string) ([]Task, error) {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil, err
	}

	data = utils.ConvertJsoncToJson(data)

	var file struct {
		Version string `json:"version"`
		Tasks   []Task `json:"tasks"`
	}

	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return file.Tasks, nil
}
