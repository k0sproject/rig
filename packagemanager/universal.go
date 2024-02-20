package packagemanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/shellescape"
)

// almost all of the package managers in the wild work the exact same way:
// <command> <action> <packages...> and <command> <update-keyword>
// with this "universal package manager" we can tackle most of them
// without implementing a new package manager for each one.
type universalPackageManager struct {
	exec.ContextRunner
	name    string
	command string
	add     string
	del     string
	update  string
}

func (u universalPackageManager) buildAndExec(ctx context.Context, kw string, packageNames ...string) error {
	if err := u.ExecContext(ctx, buildCommand(u.command, kw, packageNames...)); err != nil {
		return fmt.Errorf("failed to %s %s packages: %w", kw, u.name, err)
	}
	return nil
}

// Install given packages.
func (u universalPackageManager) Install(ctx context.Context, packageNames ...string) error {
	return u.buildAndExec(ctx, u.add, packageNames...)
}

// Remove given packages.
func (u universalPackageManager) Remove(ctx context.Context, packageNames ...string) error {
	return u.buildAndExec(ctx, u.del, packageNames...)
}

// Update the package list.
func (u universalPackageManager) Update(ctx context.Context) error {
	return u.buildAndExec(ctx, u.update)
}

func newUniversalPackageManager(runner exec.ContextRunner, name, command, add, del, update string) *universalPackageManager {
	return &universalPackageManager{
		ContextRunner: runner,
		name:          name,
		command:       command,
		add:           add,
		del:           del,
		update:        update,
	}
}

func buildCommand(basecmd, keyword string, packages ...string) string {
	cmd := &strings.Builder{}
	cmd.WriteString(basecmd)
	cmd.WriteRune(' ')
	cmd.WriteString(keyword)
	for _, pkg := range packages {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}
	return cmd.String()
}
