package byteslice_test

import (
	"bytes"
	"testing"

	"github.com/k0sproject/rig/byteslice"
	"github.com/stretchr/testify/assert"
)

func TestIndexAll(t *testing.T) {
	// Test cases
	tests := []struct {
		name     string
		slice    []byte
		sub      []byte
		expected []int
	}{
		{
			name:     "Empty slice",
			slice:    []byte{},
			sub:      []byte{0x00},
			expected: nil,
		},
		{
			name:     "Empty sub",
			slice:    []byte{0x00},
			sub:      []byte{},
			expected: nil,
		},
		{
			name:     "No matches",
			slice:    bytes.Repeat([]byte{0, 1, 3}, 100),
			sub:      []byte{2, 3},
			expected: nil,
		},
		{
			name:     "Single match",
			slice:    []byte{0x00},
			sub:      []byte{0x00},
			expected: []int{0},
		},
		{
			name:     "Multiple matches",
			slice:    []byte{0x00, 0x00, 0x00},
			sub:      []byte{0x00},
			expected: []int{0, 1, 2},
		},
		{
			name:     "Overlapping matches",
			slice:    []byte{0x00, 0x00, 0x00},
			sub:      []byte{0x00, 0x00},
			expected: []int{0},
		},
		{
			name:     "Multiple matches with gaps",
			slice:    []byte{0x00, 0x01, 0x02, 0x00, 0x01, 0x00},
			sub:      []byte{0x00, 0x01},
			expected: []int{0, 3},
		},
	}

	// Run tests
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := byteslice.IndexAll(test.slice, test.sub)
			if test.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, test.expected, result)
			}
		})
	}
}
