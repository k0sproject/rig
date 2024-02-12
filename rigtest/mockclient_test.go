package rigtest_test

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"testing"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/rigtest"
	"github.com/stretchr/testify/require"
)

func TestAddAndProcessMockCommand(t *testing.T) {
	mc := rigtest.NewMockClient()
	expectedOutput := "mock output"

	mc.AddMockCommand(regexp.MustCompile("^test$"), func(_ context.Context, _ io.Reader, stdout, _ io.Writer) error {
		stdout.Write([]byte(expectedOutput))
		return nil
	})

	runner, err := rig.NewRunner(mc)
	require.NoError(t, err)
	out, err := runner.ExecOutput("test")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	require.Equal(t, expectedOutput, out)
}

func TestCommandReception(t *testing.T) {
	mc := rigtest.NewMockClient()
	runner, err := rig.NewRunner(mc)
	require.NoError(t, err)
	_ = runner.Exec("test command")

	if !mc.Received(*regexp.MustCompile("^test")) {
		t.Errorf("Expected command 'test command' to be received")
	}

	if !mc.ReceivedSubstring("command") {
		t.Errorf("Expected to find 'test' as substring in received commands")
	}

	if !mc.ReceivedString("test command") {
		t.Errorf("Expected to find 'test command' as a received command")
	}
}

func TestCommandHistoryAndReset(t *testing.T) {
	mc := rigtest.NewMockClient()
	runner, err := rig.NewRunner(mc)
	require.NoError(t, err)
	if mc.Len() != 0 {
		t.Errorf("Expected 0 commands in history, got %d", mc.Len())
	}

	_ = runner.Exec("command1")

	if mc.Len() != 1 {
		t.Errorf("Expected 1 command in history, got %d", mc.Len())
	}

	_ = runner.Exec("command2")

	if mc.Len() != 2 {
		t.Errorf("Expected 2 commands in history, got %d", mc.Len())
	}

	mc.Reset()
	if mc.Len() != 0 {
		t.Errorf("Expected 0 commands in history after reset, got %d", mc.Len())
	}
}

func TestIsWindows(t *testing.T) {
	mc := rigtest.NewMockClient()
	require.False(t, mc.IsWindows())
	mc.Windows = true
	require.True(t, mc.IsWindows())
}

func ExampleNewMockClient() {
	mc := rigtest.NewMockClient()
	mc.AddMockCommand(regexp.MustCompile("ls"), func(_ context.Context, _ io.Reader, stdout, _ io.Writer) error {
		stdout.Write([]byte("file1\nfile2\n"))
		return nil
	})
	runner, err := rig.NewRunner(mc)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	out, err := runner.ExecOutput("ls")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println(out)
	// Output: file1
	// file2
}
