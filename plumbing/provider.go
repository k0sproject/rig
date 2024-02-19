package plumbing

import "sync"

// Factory is a function that takes a parameter of type R and returns a value of type T or an error.
type Factory[R any, T any] func(R) (T, bool)

// Provider is a generic provider of values of type T that can be initialized with a value of type R.
type Provider[R any, T any] struct {
	mu        sync.RWMutex
	factories []Factory[R, T]
	err       error
}

// Register adds a new factory to the provider.
func (p *Provider[R, T]) Register(f Factory[R, T]) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.factories = append(p.factories, f)
}

// Get retrieves the first value of type T from the Factories in the Provider.
// If none can be found, the error supplied at creation time is returned.
// The first factory that does not error is moved to the front of the list to optimize
// future lookups.
func (p *Provider[R, T]) Get(r R) (T, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, f := range p.factories {
		t, ok := f(r)
		if ok {
			if i != 0 {
				// Move the factory to the front of the list to optimize future lookups, since
				// it's likely that most of the hosts during multi-host operations will be
				// running the same kind of environment.
				p.factories[0], p.factories[i] = p.factories[i], p.factories[0]
			}
			return t, nil
		}
	}
	return *new(T), p.err
}

// GetAll retrieves all values of type T from the Factories in the Provider.
// If none that does not error can be found, the error supplied at creation time is returned.
func (p *Provider[R, T]) GetAll(r R) ([]T, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var ts []T
	for _, f := range p.factories {
		t, ok := f(r)
		if ok {
			ts = append(ts, t)
		}
	}
	if len(ts) == 0 {
		return nil, p.err
	}
	return ts, nil
}

// NewProvider creates a new instance of Provider.
// The error is returned if no factory can produce a value of type T.
func NewProvider[R any, T any](err error) *Provider[R, T] {
	return &Provider[R, T]{err: err}
}
