package remotefs_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

// requirePathErrorOp asserts that err carries a *fs.PathError naming op. The Op
// a caller reads must be the call it made, not one of the commands used to
// carry it out -- Remove reporting "stat" would name an operation the caller
// never asked for.
func requirePathErrorOp(t *testing.T, err error, op string) {
	t.Helper()
	var pathErr *fs.PathError
	require.ErrorAs(t, err, &pathErr, "the failure must be reported as an fs.PathError")
	require.Equal(t, op, pathErr.Op, "fs.PathError.Op must name the operation the caller invoked")
	require.Error(t, pathErr.Err, "the cause must be preserved under the PathError")
}
