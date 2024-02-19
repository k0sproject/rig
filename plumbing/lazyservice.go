// Package plumbing defines a generic types for the dependency injection mechanics in rig.
package plumbing

import (
	"sync"
)

var _ Service[int] = NewLazyService[int, int](NewProvider[int, int](nil), 0)

// Service is anything that can be retrieved via Get and that can fail and return an error.
type Service[T any] interface {
	Get() (T, error)
}

// providers take an exec.Runner or a subset of it and return a value of type T or an error.
type provider[S any, T any] interface {
	Get(source S) (T, error)
}

// LazyService is a generic lazy-initializer for a provider that can take a different types
// of parameter.
type LazyService[S any, T any] struct {
	once     sync.Once
	provider provider[S, T]
	source   S
	value    T
	err      error
}

// NewLazyService creates a new instance of Service with the given provider.
func NewLazyService[S any, T any](p provider[S, T], source S) *LazyService[S, T] {
	return &LazyService[S, T]{
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
