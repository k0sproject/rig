package sshconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatternMatch(t *testing.T) {
	t.Run("bare star matches anything", func(t *testing.T) {
		match, err := patternMatch("anything.example.com", "*")
		require.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("exact literal match", func(t *testing.T) {
		match, err := patternMatch("host1", "host1")
		require.NoError(t, err)
		assert.True(t, match)

		match, err = patternMatch("host1", "host2")
		require.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("star wildcard matches a suffix", func(t *testing.T) {
		match, err := patternMatch("host1.example.com", "host1.*")
		require.NoError(t, err)
		assert.True(t, match)

		match, err = patternMatch("host2.example.com", "host1.*")
		require.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("question mark matches exactly one character", func(t *testing.T) {
		match, err := patternMatch("host1", "host?")
		require.NoError(t, err)
		assert.True(t, match)

		match, err = patternMatch("host12", "host?")
		require.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("literal dots are escaped, not treated as any-character wildcards", func(t *testing.T) {
		match, err := patternMatch("hostXexample", "host.exa*")
		require.NoError(t, err)
		assert.False(t, match, "a literal dot in the pattern should not match an arbitrary character")

		match, err = patternMatch("host.example", "host.exa*")
		require.NoError(t, err)
		assert.True(t, match)
	})
}

func TestPatternMatchAll(t *testing.T) {
	t.Run("single positive pattern matches", func(t *testing.T) {
		match, err := patternMatchAll("host1", "host1")
		require.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("negated pattern alone never matches", func(t *testing.T) {
		match, err := patternMatchAll("host1", "!host2")
		require.NoError(t, err)
		assert.False(t, match)

		match, err = patternMatchAll("host2", "!host2")
		require.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("negated pattern excludes an otherwise positive match", func(t *testing.T) {
		match, err := patternMatchAll("host1", "*,!host1")
		require.NoError(t, err)
		assert.False(t, match)
	})

	t.Run("negated pattern does not affect other positive matches", func(t *testing.T) {
		match, err := patternMatchAll("host2", "*,!host1")
		require.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("comma separated patterns are split and trimmed", func(t *testing.T) {
		match, err := patternMatchAll("host2", "host1, host2, host3")
		require.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("empty and blank patterns are ignored", func(t *testing.T) {
		match, err := patternMatchAll("host1", "", "  ", "host1")
		require.NoError(t, err)
		assert.True(t, match)
	})

	t.Run("no patterns yields no match", func(t *testing.T) {
		match, err := patternMatchAll("host1")
		require.NoError(t, err)
		assert.False(t, match)
	})
}
