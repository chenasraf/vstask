package tasks

import "github.com/ktr0731/go-fuzzyfinder"

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
			return taskList[i].Detail
		}))

	if err != nil {
		return Task{}, err
	}

	return taskList[idx], nil
}
