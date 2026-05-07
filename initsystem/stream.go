package initsystem

import (
	"context"
	"fmt"
	"io"

	"github.com/k0sproject/rig/v2/cmd"
)

// streamToWriter runs command with stdout piped to w. Context cancellation is treated as a
// clean stop and returns nil — it is the expected way for callers to terminate streaming.
func streamToWriter(ctx context.Context, h cmd.ContextRunner, s, command string, w io.Writer) error {
	err := h.ExecContext(ctx, command, cmd.Stdout(w))
	if err != nil && ctx.Err() != nil {
		return nil //nolint:nilerr // context cancellation is the expected stop signal
	}
	if err != nil {
		return fmt.Errorf("failed to stream logs for service %s: %w", s, err)
	}
	return nil
}
