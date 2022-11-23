package rig

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorStringer(t *testing.T) {
	type testCase struct {
		name     string
		err      error
		expected string
	}

	for _, scenario := range []testCase{
		{
			name:     "non-wrapped error",
			err:      ErrOS,
			expected: "local os",
		},
		{
			name:     "error wrapped in error",
			err:      ErrOS.Wrap(ErrInvalidPath),
			expected: "local os: invalid path",
		},
		{
			name:     "string wrapped error",
			err:      ErrOS.Wrapf("test"),
			expected: "local os: test",
		},
		{
			name:     "double wrapped string error",
			err:      ErrOS.Wrapf("test: %w", ErrInvalidPath),
			expected: "local os: test: invalid path",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Error(t, scenario.err)
			require.Equal(t, scenario.expected, scenario.err.Error())
		})
	}
}

func TestUnwrap(t *testing.T) {
	err := ErrOS.Wrap(ErrInvalidPath)
	require.Equal(t, ErrInvalidPath, errors.Unwrap(err))
}

func TestErrorsIs(t *testing.T) {
	err := ErrOS.Wrap(ErrInvalidPath.Wrap(ErrNotFound))
	require.True(t, errors.Is(err, ErrOS))
	require.True(t, errors.Is(err, ErrInvalidPath))
	require.True(t, errors.Is(err, ErrNotFound))
	require.False(t, errors.Is(err, ErrNotConnected))
}

type testErr struct {
	msg string
}

func (t *testErr) Error() string {
	return "foo " + t.msg
}

func TestErrorsAs(t *testing.T) {
	err := ErrOS.Wrap(ErrInvalidPath.Wrap(&testErr{msg: "test"}))
	var cmp *testErr
	require.True(t, errors.As(err, &cmp))
	require.Equal(t, "local os: invalid path: foo test", err.Error())
	if errors.As(err, &cmp) {
		require.Equal(t, "foo test", cmp.Error())
	}
}
