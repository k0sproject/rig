package byteslice_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/byteslice"
	"github.com/stretchr/testify/assert"
)

func TestIndexAllPartial(t *testing.T) {
	tests := []struct {
		name            string
		slice           []byte
		sub             []byte
		expectedIndexes []int
		expectedPartial int
	}{
		{
			name:            "no match, no partial",
			slice:           []byte("xyz"),
			sub:             []byte("abc"),
			expectedIndexes: nil,
			expectedPartial: -1,
		},
		{
			name:            "full matches only",
			slice:           []byte("abcxabc"),
			sub:             []byte("abc"),
			expectedIndexes: []int{0, 4},
			expectedPartial: -1,
		},
		{
			name:            "partial match only",
			slice:           []byte("xab"),
			sub:             []byte("abc"),
			expectedIndexes: nil,
			expectedPartial: 1,
		},
		{
			name:            "full match and a distinct trailing partial",
			slice:           []byte("abcxab"),
			sub:             []byte("abc"),
			expectedIndexes: []int{0},
			expectedPartial: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			indexes, partial := byteslice.IndexAllPartial(test.slice, test.sub)
			assert.Equal(t, test.expectedIndexes, indexes)
			assert.Equal(t, test.expectedPartial, partial)
		})
	}
}
