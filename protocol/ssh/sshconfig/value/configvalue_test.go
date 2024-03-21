package value_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/protocol/ssh/sshconfig/value"
	"github.com/stretchr/testify/require"
)

func TestConfigValue(t *testing.T) {
	t.Run("SpecialStringSliceValue", func(t *testing.T) {
		ss := value.ModifiableStringListValue{}
		require.NoError(t, ss.SetString("1,2,3", ""))
		val, ok := ss.Get()
		require.True(t, ok)
		require.Equal(t, []string{"1", "2", "3"}, val)
		// set again but with + prefix
		require.NoError(t, ss.SetString("+1,2,3", ""))
		val, ok = ss.Get()
		require.True(t, ok)

		require.Equal(t, []string{"1", "2", "3"}, val, "should not append duplicates")
		require.NoError(t, ss.SetString("+4,5,6", ""))
		val, ok = ss.Get()
		require.True(t, ok)
		require.Equal(t, []string{"1", "2", "3", "4", "5", "6"}, val, "should have appended")

		// remove 3 and 4
		require.NoError(t, ss.SetString("-3,4", ""))
		val, ok = ss.Get()
		require.True(t, ok)
		require.Equal(t, []string{"1", "2", "5", "6"}, val, "should have removed 3 and 4")

		// insert 3 and 5
		require.NoError(t, ss.SetString("^3,5", ""))
		val, ok = ss.Get()
		require.True(t, ok)
		require.Equal(t, []string{"3", "5", "1", "2", "6"}, val, "should have prepended 3 and 5 and removed the old 5")
	})
	t.Run("SpecialStringSliceValue with pattern", func(t *testing.T) {
		ss := value.ModifiableStringListValue{}
		require.NoError(t, ss.SetString("one,two,three", ""))
		val, ok := ss.Get()
		require.True(t, ok)
		require.Equal(t, []string{"one", "two", "three"}, val)
		// remove all that start with t
		require.NoError(t, ss.SetString("-t*", ""))
		val, ok = ss.Get()
		require.True(t, ok)
		require.Equal(t, []string{"one"}, val, "should have removed two and three")
	})
}
