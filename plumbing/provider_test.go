package plumbing_test

import (
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider(t *testing.T) {
	fallbackErr := errors.New("no factory available")
	p := plumbing.NewProvider[int, string](fallbackErr)
	assert.NotNil(t, p)
}

func TestRegisterAndGet(t *testing.T) {
	p := plumbing.NewProvider[int, string](nil)

	// Mock factory
	factory := func(i int) (string, bool) {
		return "value", true
	}
	p.Register(factory)

	value, err := p.Get(1)
	require.NoError(t, err)
	assert.Equal(t, "value", value)
}

func TestGetNoFactory(t *testing.T) {
	err := errors.New("no factory available")
	p := plumbing.NewProvider[int, string](err)

	value, err := p.Get(1)
	require.Error(t, err)
	assert.Empty(t, value)
}

func TestGetAll(t *testing.T) {
	p := plumbing.NewProvider[int, string](nil)

	// Mock factories
	factory1 := func(i int) (string, bool) {
		return "value1", true
	}
	factory2 := func(i int) (string, bool) {
		return "value2", true
	}
	p.Register(factory1)
	p.Register(factory2)

	values, err := p.GetAll(1)
	require.NoError(t, err)
	assert.Equal(t, []string{"value1", "value2"}, values)
}

func TestGetAllNoFactory(t *testing.T) {
	err := errors.New("no factory available")
	p := plumbing.NewProvider[int, string](err)

	values, err := p.GetAll(1)
	require.Error(t, err)
	assert.Nil(t, values)
}
