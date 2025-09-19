package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"path"

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
