package plumbing

import "sync"

// Factory is a function that takes a parameter of type R and returns a value of
// type T along with a boolean reporting whether it could handle R.
//
// A Factory may be called concurrently with itself, for the same input as well as
// for different ones, so it must not depend on being called one at a time.
type Factory[R any, T any] func(R) (T, bool)

// Provider is a generic provider of values of type T that can be initialized with a value of type R.
type Provider[R any, T any] struct {
	mu        sync.RWMutex
	factories []Factory[R, T]
	err       error
}

// Register adds a new factory to the provider.
//
// Factories are consulted in registration order and the first one to match wins.
// Prefer factories that decide for themselves whether they apply to an input over
// relying on that order: a factory matching a superset of another's inputs should
// exclude the cases the more specific one handles, the way os.ResolveLinuxCompat
// stands down for any host os-release can identify. A registry stays open for
// registration, so a broad factory registered early keeps winning over any more
// specific one a caller adds later.
func (p *Provider[R, T]) Register(f Factory[R, T]) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.factories = append(p.factories, f)
}

// Get returns the value from the first factory that reports a match, in
// registration order. If none of them match, the error supplied at creation time
// is returned.
//
// The order in which factories are consulted never changes, so the result for a
// given input does not depend on what was looked up before it.
func (p *Provider[R, T]) Get(input R) (T, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, f := range p.factories {
		if t, ok := f(input); ok {
			return t, nil
		}
	}

	return *new(T), p.err
}

// GetAll returns the values from every factory that reports a match, in
// registration order. If none of them match, the error supplied at creation time
// is returned.
func (p *Provider[R, T]) GetAll(input R) ([]T, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var values []T
	for _, f := range p.factories {
		if t, ok := f(input); ok {
			values = append(values, t)
		}
	}
	if len(values) == 0 {
		return nil, p.err
	}

	return values, nil
}

// NewProvider creates a new instance of Provider.
// The error is returned if no factory can produce a value of type T.
func NewProvider[R any, T any](err error) *Provider[R, T] {
	return &Provider[R, T]{err: err}
}
