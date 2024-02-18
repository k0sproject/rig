// Package plumbing defines a generic types for the dependency injection mechanics in rig.
package plumbing

import (
	"sync"
)

// providers take an exec.Runner or a subset of it and return a value of type T or an error.
type provider[R any, T any] interface {
	Get(runner R) (T, error)
}

// LazyService is a generic lazy-initializer for a provider that can take a different types
// of parameter.
type LazyService[R any, T any] struct {
	once     sync.Once
	provider provider[R, T]
	source   R
	value    T
	err      error
}

// NewLazyService creates a new instance of Service with the given provider.
func NewLazyService[R any, T any](p provider[R, T], source R) *LazyService[R, T] {
	return &LazyService[R, T]{
		provider: p,
		source:   source,
	}
}

// Get retrieves the service value, initializing it if necessary.
func (s *LazyService[R, T]) Get() (T, error) {
	s.once.Do(func() {
		s.value, s.err = s.provider.Get(s.source)
	})
	return s.value, s.err
}
