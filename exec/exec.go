// Package exec provides command exeecution capabilities for rig connections.
package exec

// DisableRedact will disable all redaction of sensitive data.
var DisableRedact = false

// Waiter is a process that can be waited to finish.
type Waiter interface {
	Wait() error
}
