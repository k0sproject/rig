// Package retry provides context based retry functionality for functions.
package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrPanic is returned when a panic is rescued.
	ErrPanic = errors.New("panic")
	// ErrAbort is returned when retrying an operation will not result in a
	// different outcome.
	ErrAbort = errors.New("operation can not be completed")
)

// Options for retry.
type Options struct {
	rescuePanic   bool
	delay         time.Duration
	backoffFactor float64
	maxRetries    int
	continueOnErr func(error) bool
}

// NewOptions returns a new Options with the given options applied.
func NewOptions(opts ...Option) Options {
	options := Options{
		rescuePanic: false,
		delay:       2 * time.Second,
		continueOnErr: func(err error) bool {
			return !errors.Is(err, ErrAbort)
		},
		backoffFactor: 1.0,
	}

	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// Option is a functional option function for Options.
type Option func(*Options)

// RescuePanic is a functional option that controls if panics should be rescued.
func RescuePanic() Option {
	return func(o *Options) {
		o.rescuePanic = true
	}
}

// Delay is a functional option that sets the delay between retries. The default
// is 2 seconds.
func Delay(d time.Duration) Option {
	return func(o *Options) {
		o.delay = d
	}
}

// MaxRetries is a functional option that sets the maximum number of retries. The default
// is to retry indefinitely or until the context is done or canceled.
func MaxRetries(n int) Option {
	return func(o *Options) {
		o.maxRetries = n
	}
}

// Backoff is a functional option that sets the backoff factor. On each attempt the
// delay is multiplied by this factor. The default is 1.0.
func Backoff(f float64) Option {
	return func(o *Options) {
		o.backoffFactor = f
	}
}

// If is a functional option that sets the function to determine if
// an error should continue the retry. If the function returns true,
// the retry will continue.
func If(f func(error) bool) Option {
	return func(o *Options) {
		o.continueOnErr = f
	}
}

type retrier struct {
	fn    func() error
	ctxFn func(context.Context) error
	opts  Options
}

// Do runs the function until it returns nil or the context is done or canceled.
func Do(ctx context.Context, fn func() error, opts ...Option) error {
	r := &retrier{
		fn:   fn,
		opts: NewOptions(opts...),
	}
	return r.do(ctx)
}

// DoWithContext runs the function and passes the context to it until it returns nil
// or the context is done or canceled.
func DoWithContext(ctx context.Context, fn func(context.Context) error, opts ...Option) error {
	r := &retrier{
		ctxFn: fn,
		opts:  NewOptions(opts...),
	}
	return r.do(ctx)
}

// DoFor retries the function until it returns nil or the given duration has passed.
func DoFor(d time.Duration, fn func() error, opts ...Option) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return Do(ctx, fn, opts...)
}

// DoForWithContext retries the function and passes the context to it until it returns nil
// or the given duration has passed.
func DoForWithContext(d time.Duration, fn func(context.Context) error, opts ...Option) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return DoWithContext(ctx, fn, opts...)
}

// Get is a generic alternative of Do that returns the result of a function that returns a value and an error.
func Get[T any](ctx context.Context, fn func() (T, error), opts ...Option) (T, error) {
	var result T
	err := Do(ctx, func() error {
		var err error
		result, err = fn()
		return err
	}, opts...)
	return result, err
}

// GetWithContext is a generic alternative of DoWithContext that returns the result of a function that returns a value and an error.
func GetWithContext[T any](ctx context.Context, fn func(context.Context) (T, error), opts ...Option) (T, error) {
	var result T
	err := DoWithContext(ctx, func(c context.Context) error {
		var err error
		result, err = fn(c)
		return err
	}, opts...)
	return result, err
}

func (r *retrier) doOnce(ctx context.Context) (err error) {
	if r.opts.rescuePanic {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("%w: %v", ErrPanic, p)
			}
		}()
	}

	if r.ctxFn != nil {
		err = r.ctxFn(ctx)
	} else {
		err = r.fn()
	}

	return err
}

func (r *retrier) do(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("retry: context done or canceled before first attempt: %w", ctx.Err())
	}
	attempt := 0
	for {
		attempt++
		err := r.doOnce(ctx)
		if err == nil {
			return nil
		}

		if !r.opts.continueOnErr(err) {
			return fmt.Errorf("retry: abort condition reached after %d attempts: %w", attempt, err)
		}

		if r.opts.maxRetries > 0 && attempt >= r.opts.maxRetries {
			return fmt.Errorf("retry: max retries reached: %w", err)
		}

		select {
		// sleep for delay * (attempt * backoff factor)
		case <-time.After(time.Duration(float64(r.opts.delay) * (r.opts.backoffFactor * float64(attempt)))):
		case <-ctx.Done():
			return fmt.Errorf("retry: context done after %d attempts: %w: %w", attempt, ctx.Err(), err)
		}
	}
}
