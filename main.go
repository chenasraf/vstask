package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/chenasraf/vstask/runner"
	"github.com/chenasraf/vstask/tasks"
	"github.com/chenasraf/vstask/utils"
)

//go:embed version.txt
var appVersion []byte // appVersion is embedded from version.txt and contains the application version.

func main() {
	utils.SetVersion(strings.TrimSpace(string(appVersion)))
	args := os.Args[1:]
	useDefaults := false
	filteredArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--use-defaults" {
			useDefaults = true
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}
	args = filteredArgs

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
		task, err := tasks.FindTask(taskList, args[0])
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		err = runner.RunTaskWithOptions(task, runner.RunTaskOptions{UseDefaults: useDefaults})
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
	err = runner.RunTaskWithOptions(selected, runner.RunTaskOptions{UseDefaults: useDefaults})
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
