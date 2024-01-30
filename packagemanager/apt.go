package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Apt struct {
	exec.ContextRunner
}

func (a *Apt) Install(ctx context.Context, packageNames ...string) error {
	if err := a.ExecContext(ctx, "DEBIAN_FRONTEND=noninteractive APT_LISTCHANGES_FRONTEND=none %s", buildCommand("install -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to install apt packages: %w", err)
	}

	return nil
}

func (a *Apt) Remove(ctx context.Context, packageNames ...string) error {
	if err := a.ExecContext(ctx, "DEBIAN_FRONTEND=noninteractive %s", buildCommand("remove -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove apt packages: %w", err)
	}
	return nil
}

func (a *Apt) Update(ctx context.Context) error {
	if err := a.ExecContext(ctx, buildCommand("update")); err != nil {
		return fmt.Errorf("failed to update apt: %w", err)
	}
	return nil
}

func RegisterApt(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v apt-get") != nil {
			return nil
		}
		return &Apt{c}
	})
}
