package rigfs

import (
	"io"
	"io/fs"
	"sort"
)

type dirEntryBuffer struct {
	entries []fs.DirEntry
}

func newDirEntryBuffer(entries []fs.DirEntry) *dirEntryBuffer {
	sort.Slice(entries, func(i, j int) bool {
		isDirI, isDirJ := entries[i].IsDir(), entries[j].IsDir()

		// If both are directories or files, sort alphabetically
		if isDirI == isDirJ {
			return entries[i].Name() < entries[j].Name()
		}

		// Otherwise, directories should come first
		return isDirI
	})

	return &dirEntryBuffer{entries: entries}
}

// Next returns the next n entries from the buffer.
// Subsequent calls on the same file will yield further DirEntry values.
// When there are no more entries, io.EOF is returned.
// A negative count returns all the remaining entries in the buffer.
func (b *dirEntryBuffer) Next(n int) ([]fs.DirEntry, error) {
	if len(b.entries) == 0 {
		return nil, io.EOF
	}

	if n == 0 {
		return nil, nil
	}

	if n < 0 || n > len(b.entries) {
		n = len(b.entries)
	}

	// Retrieve the next n entries
	entries := b.entries[:n]
	b.entries = b.entries[n:]
	return entries, nil
}
