package shellescape_test

import (
	"os"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/sh/shellescape"
	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	originalValue := os.Getenv("TEST_VAR")
	defer os.Setenv("TEST_VAR", originalValue)
	os.Setenv("TEST_VAR", "test value")

	t.Run("dollar", func(t *testing.T) {
		result, err := shellescape.Expand("show me the $TEST_VAR.")
		require.NoError(t, err)
		require.Equal(t, "show me the test value.", result)
	})
	t.Run("curly", func(t *testing.T) {
		result, err := shellescape.Expand("show me the ${TEST_VAR}.")
		require.NoError(t, err)
		require.Equal(t, "show me the test value.", result)
	})
	t.Run("command", func(t *testing.T) {
		result, err := shellescape.Expand("show me the $(echo test $(echo value)).")
		require.NoError(t, err)
		require.Equal(t, "show me the test value.", result)
	})
	t.Run("command with curly", func(t *testing.T) {
		result, err := shellescape.Expand("show me the $(echo $(echo ${TEST_VAR})).")
		require.NoError(t, err)
		require.Equal(t, "show me the test value.", result)
	})
}

func TestExpandParamExpansion(t *testing.T) {
	originalValue := os.Getenv("TEST_VAR")
	defer os.Setenv("TEST_VAR", originalValue)
	os.Setenv("TEST_VAR", "test value")

	originalValue2 := os.Getenv("TEST_VAR2")
	defer os.Setenv("TEST_VAR2", originalValue2)
	os.Unsetenv("TEST_VAR2")

	t.Run("simple", func(t *testing.T) {
		result, err := shellescape.Expand("show me the ${TEST_VAR}.")
		require.NoError(t, err)
		require.Equal(t, "show me the test value.", result)
	})

	t.Run("colon", func(t *testing.T) {
		t.Run("default", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR:-default}.")
			require.NoError(t, err)
			require.Equal(t, "show me the test value.", result)
		})
		t.Run("default with empty", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR:-}.")
			require.NoError(t, err)
			require.Equal(t, "show me the test value.", result)
		})
		t.Run("default with empty and space", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR:- }.")
			require.NoError(t, err)
			require.Equal(t, "show me the test value.", result)
		})
		t.Run("default with unset", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR2:-default}.")
			require.NoError(t, err)
			require.Equal(t, "show me the default.", result)
		})
		t.Run("anti-default with unset", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR2:+default}.")
			require.NoError(t, err)
			require.Equal(t, "show me the .", result)
			result, err = shellescape.Expand("show me the ${TEST_VAR:+default}.")
			require.NoError(t, err)
			require.Equal(t, "show me the default.", result)
		})
		t.Run("offset", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR:2}.")
			require.NoError(t, err)
			require.Equal(t, "show me the st value.", result)
		})
		t.Run("offset with empty", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR2:2}.")
			require.NoError(t, err)
			require.Equal(t, "show me the .", result)
		})
		t.Run("offset with negative", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR: -2}.")
			require.NoError(t, err)
			require.Equal(t, "show me the ue.", result)
		})
		t.Run("offset with negative and length", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR: -3:2}.")
			require.NoError(t, err)
			require.Equal(t, "show me the lu.", result)
		})
		t.Run("offset with length", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${TEST_VAR:2:2}.")
			require.NoError(t, err)
			require.Equal(t, "show me the st.", result)
		})
	})

	t.Run("length", func(t *testing.T) {
		t.Run("length", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${#TEST_VAR}.")
			require.NoError(t, err)
			require.Equal(t, "show me the 10.", result)
		})
		t.Run("length with empty", func(t *testing.T) {
			result, err := shellescape.Expand("show me the ${#TEST_VAR2}.")
			require.NoError(t, err)
			require.Equal(t, "show me the 0.", result)
		})
	})

	t.Run("variable names", func(t *testing.T) {
		if orig, ok := os.LookupEnv("TEST_VAR2"); ok {
			defer os.Setenv("TEST_VAR2", orig)
		} else {
			defer os.Unsetenv("TEST_VAR2")
		}
		os.Setenv("TEST_VAR2", "test value2")
		result, err := shellescape.Expand("show me the ${!TEST_VA*}")
		require.NoError(t, err)
		result = strings.TrimPrefix(result, "show me the ")
		result = strings.TrimSuffix(result, ".")
		items := strings.Split(result, " ")
		require.Contains(t, items, "TEST_VAR")
		require.Contains(t, items, "TEST_VAR2")
	})
}
