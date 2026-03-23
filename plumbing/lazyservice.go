// Package plumbing defines generic types for the dependency injection mechanisms in rig.
package plumbing

import (
	"sync"
)

var _ Service[int] = NewLazyService(NewProvider[int, int](nil).Get, 0)

// Service is anything that can be retrieved via Get and that can fail and return an error.
type Service[T any] interface {
	Get() (T, error)
}

// LazyService is a generic lazy-initializer that calls a provider function once
// and memoizes the result.
type LazyService[S any, T any] struct {
	once   sync.Once
	get    func(S) (T, error)
	source S
	value  T
	err    error
}

// NewLazyService creates a new instance of LazyService with the given provider function.
func NewLazyService[S any, T any](get func(S) (T, error), source S) *LazyService[S, T] {
	return &LazyService[S, T]{
		get:    get,
		source: source,
	}
}

// Get retrieves the service value, initializing it if necessary.
func (s *LazyService[S, T]) Get() (T, error) {
	s.once.Do(func() {
		s.value, s.err = s.get(s.source)
	})
	return s.value, s.err
}
