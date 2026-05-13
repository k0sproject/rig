package rig

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/v2/protocol"
)

// Reboot triggers an immediate restart of the remote host. The method
// returns as soon as the reboot has been requested; the caller is
// responsible for polling [Client.IsConnected] until the host goes down and
// comes back.
//
// Callers that need elevated privileges should invoke this on a sudo-decorated
// client (for example c.Sudo().Reboot(ctx)).
func (c *Client) Reboot(ctx context.Context) error {
	if c.connection == nil {
		return fmt.Errorf("%w: connection not properly initialized", protocol.ErrNonRetryable)
	}
	if err := c.FS().Reboot(ctx); err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	return nil
}
