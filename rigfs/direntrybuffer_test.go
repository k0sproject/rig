package rigfs

import (
	"errors"
	"io"
	"io/fs"
	"testing"
)

func TestDirEntryBuffer(t *testing.T) {
	// Create mock DirEntry slices for testing
	mockEntries := []fs.DirEntry{
		mockDirEntry{name: "file1"},
		mockDirEntry{name: "file2"},
		mockDirEntry{name: "file3"},
	}

	// Test cases
	tests := []struct {
		name        string
		n           int
		initEntries []fs.DirEntry
		wantEntries []int
		wantErr     []error
	}{
		{"Empty Buffer", 1, []fs.DirEntry{}, []int{0}, []error{io.EOF}},
		{"Single Call", 2, mockEntries, []int{2}, []error{nil}},
		{"Multiple Calls", 1, mockEntries, []int{1, 1, 1, 0}, []error{nil, nil, nil, io.EOF}},
		{"Exact Count", 3, mockEntries, []int{3}, []error{nil}},
		{"Negative Count", -1, mockEntries, []int{3}, []error{nil}},
		{"End of Buffer", 10, mockEntries, []int{3, 0}, []error{nil, io.EOF}},
		{"Zero Count", 0, mockEntries, []int{0}, []error{nil}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buffer := newDirEntryBuffer(tc.initEntries)
			for i, want := range tc.wantEntries {
				entries, err := buffer.Next(tc.n)
				if len(entries) != want {
					t.Errorf("Call %d: got %d entries, want %d", i+1, len(entries), want)
				}
				if !errors.Is(err, tc.wantErr[i]) {
					t.Errorf("Call %d: got error %v, want %v", i+1, err, tc.wantErr[i])
				}
			}
		})
	}
}

// mockDirEntry is a mock implementation of fs.DirEntry for testing purposes.
type mockDirEntry struct {
	name string
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return false }
func (m mockDirEntry) Type() fs.FileMode          { return 0 }
func (m mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }
