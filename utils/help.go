package utils

import (
	"fmt"
)

func PrintHelp() {
	fmt.Println("Usage: vstask [--use-defaults] [task-name]")
	fmt.Println("Options:")
	fmt.Println("  -h, --help         Show this help message")
	fmt.Println("      --use-defaults Use declared defaults for task inputs")
}

func PrintVersion() {
	fmt.Println(AppVersion)
}

// AppVersion is the current version of the application.
var AppVersion string

// SetVersion sets the application version.
func SetVersion(v string) {
	AppVersion = v
}
