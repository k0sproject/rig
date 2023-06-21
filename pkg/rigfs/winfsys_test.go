package rigfs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatPath(t *testing.T) {
	fsys := NewWindowsFsys(nil)
	require.Equal(t, "\"C:\\Users\\Public\\Documents\\foo.txt\"", fsys.formatPath("C:\\Users\\Public", "Documents\\foo.txt"))
	require.Equal(t, "\"C:\\Users\\Public\\Documents\\foo.txt\"", fsys.formatPath("C:\\Users\\Public\\Documents\\foo.txt"))
	require.Equal(t, "\"Users\\Public\\Documents\\foo.txt\"", fsys.formatPath("Users\\Public\\Documents\\foo.txt"))
	require.Equal(t, "\"Users\\Public\\Documents\\foo.txt\"", fsys.formatPath("Users\\Public", "Documents\\foo.txt"))

	require.Equal(t, "\"\\Users\\Public\\Documents\\foo.txt\"", fsys.formatPath("/Users/Public/Documents/foo.txt"))
	require.Equal(t, "\"Users\\Public\\Documents\\foo.txt\"", fsys.formatPath("Users/Public", "Documents/foo.txt"))

	require.Equal(t, "\"\\\\foo\\bar\\baz.txt\"", fsys.formatPath("\\\\foo\\bar\\baz.txt"))
}
