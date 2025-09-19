package main

import (
	"fmt"
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
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		task, found := lo.Find(taskList, func(t tasks.Task) bool {
			return t.Label == args[0]
		})
		if !found {
			fmt.Println("Error:", "Task not found: "+args[0])
			os.Exit(1)
		}
		err = runner.RunTask(task)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	selected, err := tasks.PromptForTask()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	if selected.IsEmpty() {
		fmt.Println("No task selected.")
		os.Exit(1)
	}
	err = runner.RunTask(selected)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
