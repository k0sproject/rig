package plumbing

import (
	"slices"
	"sync"
)

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
// Factories are consulted in registration order and the first one to match wins,
// after any added with RegisterFirst.
//
// Prefer factories that decide for themselves whether they apply to an input over
// relying on that order: a factory matching a superset of another's inputs should
// exclude the cases the more specific one handles, the way os.ResolveLinuxCompat
// stands down for any host os-release can identify. A registry a package exports
// stays open for registration, so it cannot know where a caller's factory will
// land, and a broad factory registered early otherwise keeps winning over a more
// specific one added later.
func (p *Provider[R, T]) Register(f Factory[R, T]) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.factories = append(p.factories, f)
}

// RegisterFirst adds a factory at the front of the list, ahead of every factory
// already registered and every factory registered afterwards with Register. Where
// it is called more than once the most recent call is the one consulted first, so
// the last caller to ask for precedence gets it.
//
// It is for a caller that has to take precedence over a factory the registry
// already holds: one matching a superset of the inputs their own factory handles,
// where self-exclusion is not available to them because the factory that would
// have to stand down is not theirs to change. A package registering factories into
// its own registry should use Register and have them decide from the input whether
// they apply.
//
// The result of a lookup may be memoized by the caller of Get, so register before
// the first lookup rather than after.
func (p *Provider[R, T]) RegisterFirst(f Factory[R, T]) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.factories = slices.Insert(p.factories, 0, f)
}

// Get returns the value from the first factory that reports a match, in
// registration order with any RegisterFirst factories ahead of the rest. If none
// of them match, the error supplied at creation time is returned.
//
// A lookup never reorders the factories, so the result for a given input does not
// depend on what was looked up before it.
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
// registration order with any RegisterFirst factories ahead of the rest. If none
// of them match, the error supplied at creation time is returned.
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
