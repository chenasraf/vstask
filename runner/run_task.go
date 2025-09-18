package runner

import (
	"fmt"

	"github.com/chenasraf/vstask/tasks"
)

func RunTask(task tasks.Task) error {
	fmt.Println("Running task:", task.Label)
	return nil
}
