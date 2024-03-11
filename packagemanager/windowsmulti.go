package packagemanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
)

// WindowsMultiManager combines all found windows package managers and tries to manage packaes through all of them.
// This is done because there is no single one package manager to rule them all for windows.
type WindowsMultiManager struct {
	cmd.ContextRunner
	managers []PackageManager
}

// ErrNoWindowsPackageManager is returned when no windows package manager is found.
var ErrNoWindowsPackageManager = errors.New("no windows package manager found")

// Install the given packages.
func (w *WindowsMultiManager) Install(ctx context.Context, packageNames ...string) error {
	if len(w.managers) == 0 {
		return ErrNoWindowsPackageManager
	}

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

// Remove the given packages.
func (w *WindowsMultiManager) Remove(ctx context.Context, packageNames ...string) error {
	if len(w.managers) == 0 {
		return ErrNoWindowsPackageManager
	}

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

// Update the package lists in all the package managers.
func (w *WindowsMultiManager) Update(ctx context.Context) error {
	if len(w.managers) == 0 {
		return ErrNoWindowsPackageManager
	}

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

// NewWindowsMultiManager creates a new windows multi package manager.
func NewWindowsMultiManager(c cmd.ContextRunner) PackageManager {
	winRepo := NewProvider()
	RegisterWinget(winRepo)
	RegisterChocolatey(winRepo)
	RegisterScoop(winRepo)
	managers, _ := winRepo.GetAll(c)
	return &WindowsMultiManager{ContextRunner: c, managers: managers}
}

// RegisterWindowsMultiManager registers the windows multi package manager to a repository.
func RegisterWindowsMultiManager(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		return NewWindowsMultiManager(c), true
	})
}
