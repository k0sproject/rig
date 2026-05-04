package rig

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// DefaultPasswordCallback is a default implementation for PasswordCallback.
func DefaultPasswordCallback() (string, error) {
	fmt.Print("Enter passphrase: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(pass), nil
}
