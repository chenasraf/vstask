package main

import (
	"errors"
	"os"

	"github.com/chenasraf/vstask/runner"
	"github.com/chenasraf/vstask/tasks"
	"github.com/samber/lo"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		if args[0] == "--help" || args[0] == "-h" {
			// PrintHelp()
			os.Exit(0)
		}
		taskList, err := tasks.GetTasks()
		if err != nil {
			panic(err)
		}
		task, found := lo.Find(taskList, func(t tasks.Task) bool {
			return t.Label == args[0]
		})
		if !found {
			panic(errors.New("task not found: " + args[0]))
		}
		runner.RunTask(task)
		os.Exit(0)
	}
	selected, err := tasks.PromptForTask()
	if err != nil {
		panic(err)
	}
	runner.RunTask(selected)
}
