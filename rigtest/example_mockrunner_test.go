package rigtest_test

import (
	"fmt"

	"github.com/k0sproject/rig/rigtest"
)

// ExampleMockRunner demonstrates how to mock a connection to match the "ls" command and return a list of fake filenames.
func ExampleMockRunner() {
	runner := rigtest.NewMockRunner()
	runner.AddCommand(rigtest.Equal("ls"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stdout, "file1")
		fmt.Fprintln(a.Stdout, "file2")
		return nil
	})
	out, err := runner.ExecOutput("ls")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println(out)
	// Output:
	// file1
	// file2
}
