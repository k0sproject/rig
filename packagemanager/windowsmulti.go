package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

// WindowsMultiManager combines all found windows package managers and tries to manage packaes through all of them.
// This is done because there is no single one package manager to rule them all for windows.
type WindowsMultiManager struct {
	exec.ContextRunner
	managers []PackageManager
}

func (w *WindowsMultiManager) Install(ctx context.Context, packageNames ...string) error {
	var lastErr error
	for _, pkg := range packageNames {
		for _, manager := range w.managers {
			err := manager.Install(ctx, pkg)
			if err == nil {
				break
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to install packages, final error: %w", lastErr)
	}
	return nil
}

func (w *WindowsMultiManager) Remove(ctx context.Context, packageNames ...string) error {
	var lastErr error
	for _, pkg := range packageNames {
		for _, manager := range w.managers {
			err := manager.Remove(ctx, pkg)
			if err == nil {
				break
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to uninstall packages, final error: %w", lastErr)
	}
	return nil
}

func (w *WindowsMultiManager) Update(ctx context.Context) error {
	var lastErr error
	for _, manager := range w.managers {
		err := manager.Update(ctx)
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to update some package managers, final error: %w", lastErr)
	}
	return nil
}

func RegisterWindowsMultiManager(repository *Repository) {
	winRepo := NewRepository()
	RegisterWinget(winRepo)
	RegisterChocolatey(winRepo)
	RegisterScoop(winRepo)
	repository.Register(func(c exec.ContextRunner) PackageManager {
		managers := winRepo.getAll(c)
		if len(managers) == 0 {
			return nil
		}
		return &WindowsMultiManager{
			ContextRunner: c,
			managers:      managers,
		}
	})
}
