package plumbing_test

import (
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider[T any] struct {
	value T
	err   error
	calls int
}

func (m *mockProvider[T]) Get(_ int) (T, error) {
	m.calls++
	return m.value, m.err
}

func TestLazyService(t *testing.T) {
	mock := &mockProvider[int]{value: 42, err: nil}
	ls := plumbing.NewLazyService[int, int](mock, 0)

	value, err := ls.Get()
	require.NoError(t, err)
	assert.Equal(t, 42, value)
	assert.Equal(t, 1, mock.calls)

	value, err = ls.Get()
	require.NoError(t, err)
	assert.Equal(t, 42, value)
	assert.Equal(t, 1, mock.calls, "lazy initialization should not call the provider multiple times")
}

func TestLazyServiceWithError(t *testing.T) {
	expectedErr := errors.New("error")
	mock := &mockProvider[int]{value: 0, err: expectedErr}
	ls := plumbing.NewLazyService[int, int](mock, 0)

	_, err := ls.Get()
	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, 1, mock.calls)

	_, err = ls.Get()
	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, 1, mock.calls, "lazy initialization should not call the provider multiple times")
}
