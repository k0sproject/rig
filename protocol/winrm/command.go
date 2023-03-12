package winrm

import (
	"fmt"
	"io"
	"sync"

	"github.com/masterzen/winrm"
)

var ErrCommandFailed = fmt.Errorf("command failed")

type Xommand struct {
	sh     *winrm.Shell
	cmd    *winrm.Command
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// Wait blocks until the command finishes
func (c *Xommand) Wait() error {
	var wg sync.WaitGroup
	defer c.sh.Close()
	defer c.cmd.Close()
	if c.stdin == nil {
		c.cmd.Stdin.Close()
	} else {
		wg.Add(1)
		go func() {
			defer c.cmd.Stdin.Close()
			defer wg.Done()
			_, _ = io.Copy(c.cmd.Stdin, c.stdin)
		}()
	}
	if c.stdout != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(c.stdout, c.cmd.Stdout)
		}()
	}
	if c.stderr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(c.stderr, c.cmd.Stderr)
		}()
	}

	c.cmd.Wait()
	wg.Wait()

	if c.cmd.ExitCode() != 0 {
		return fmt.Errorf("%w: exit code %d", ErrCommandFailed, c.cmd.ExitCode())
	}

	return nil
}
