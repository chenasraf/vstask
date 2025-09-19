package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/chenasraf/vstask/runner"
	"github.com/chenasraf/vstask/tasks"
	"github.com/chenasraf/vstask/utils"
	"github.com/samber/lo"
)

//go:embed version.txt
var appVersion []byte // appVersion is embedded from version.txt and contains the application version.

func main() {
	utils.SetVersion(strings.TrimSpace(string(appVersion)))
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			utils.PrintHelp()
			os.Exit(0)
		case "-v", "--version":
			utils.PrintVersion()
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
