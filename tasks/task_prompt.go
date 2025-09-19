package tasks

import (
	"bytes"
	"os"

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
			// data, err := json.MarshalIndent(taskList[i], "", "  ")
			enc.SetIndent("", "  ")
			err := enc.Encode(taskList[i])
			if err != nil {
				return "Error displaying task details"
			}
			return buf.String()
		}))

	if err != nil {
		return Task{}, err
	}

	return taskList[idx], nil
}
