// Package abort only hosts the ErrAbort error type.
package abort

import "errors"

// ErrAbort is returned when an operation that is possibly retried, such as a connection attempt, should be aborted
// since it is not feasible to expect a successful outcome.
var ErrAbort = errors.New("operation can not be completed")
