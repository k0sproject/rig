package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryDoSuccessWithMaxRetries(t *testing.T) {
	ctx := context.Background()
	var attempts int
	err := retry.Do(ctx, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("fail")
		}
		return nil
	}, retry.MaxRetries(5), retry.Delay(10*time.Millisecond))

	require.NoError(t, err)
	assert.Equal(t, 3, attempts, "Expected 3 attempts for success")
}

func TestRetryDoWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var attempts int
	err := retry.DoWithContext(ctx, func(ctx context.Context) error {
		attempts++
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("fail")
	}, retry.MaxRetries(5), retry.Delay(10*time.Millisecond))

	require.ErrorIs(t, err, context.Canceled, "Expected context.Canceled error")
	assert.Zero(t, attempts, "Expected no attempts")
}

func TestRetryGet(t *testing.T) {
	ctx := context.Background()
	t.Run("int", func(t *testing.T) {
		var attempts int
		value, err := retry.Get(ctx, func() (int, error) {
			attempts++
			if attempts < 3 {
				return 0, errors.New("fail")
			}
			return 42, nil
		}, retry.MaxRetries(5), retry.Delay(10*time.Millisecond))

		require.NoError(t, err)
		assert.Equal(t, 42, value, "Expected value 42")
		assert.Equal(t, 3, attempts, "Expected 3 attempts for success")
	})
	t.Run("string", func(t *testing.T) {
		var attempts int
		value, err := retry.Get(ctx, func() (string, error) {
			attempts++
			if attempts < 3 {
				return "", errors.New("fail")
			}
			return "success", nil
		}, retry.MaxRetries(5), retry.Delay(10*time.Millisecond))

		require.NoError(t, err)
		assert.Equal(t, "success", value, "Expected value 'success'")
		assert.Equal(t, 3, attempts, "Expected 3 attempts for success")
	})
}

func TestRetryGetWithContext(t *testing.T) {
	ctx := context.Background()
	var attempts int
	value, err := retry.GetWithContext(ctx, func(_ context.Context) (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("fail")
		}
		return "success", nil
	}, retry.MaxRetries(3), retry.Delay(10*time.Millisecond))

	require.NoError(t, err)
	assert.Equal(t, "success", value, "Expected value 'success'")
	assert.Equal(t, 2, attempts, "Expected 2 attempts for success")
}

func TestRetryDoWithTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var attempts int
	err := retry.Do(ctx, func() error {
		attempts++
		return errors.New("fail")
	}, retry.Delay(10*time.Millisecond))
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded, "Expected context.DeadlineExceeded error")
	assert.True(t, attempts > 1, "Expected at least 2 attempts")
}

func TestRetryDoWithCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var attempts int
	err := retry.Do(ctx, func() error {
		attempts++
		return errors.New("fail")
	}, retry.Delay(10*time.Millisecond))
	require.Error(t, err)
	assert.True(t, attempts > 1, "Expected at least 2 attempts")
	require.ErrorIs(t, err, context.Canceled, "Expected context.Canceled error")
}

func TestRetryDoWithPanic(t *testing.T) {
	ctx := context.Background()
	err := retry.Do(ctx, func() error {
		panic("panic")
	}, retry.MaxRetries(1), retry.RescuePanic())
	require.Error(t, err)
	require.ErrorIs(t, err, retry.ErrPanic, "Expected retry.ErrPanic error")
}

func TestRetryDoWithIf(t *testing.T) {
	ctx := context.Background()
	var attempts int
	err := retry.Do(ctx, func() error {
		attempts++
		return errors.New("fail")
	}, retry.MaxRetries(5), retry.Delay(10*time.Millisecond), retry.If(func(err error) bool {
		return err.Error() == "fail" && attempts < 3
	}))

	require.Error(t, err)
	assert.Equal(t, 3, attempts, "Expected 3 attempts for success")
}

func TestRetryDoWithBackoff(t *testing.T) {
	ctx := context.Background()
	var attempts int
	start := time.Now()
	err := retry.Do(ctx, func() error {
		attempts++
		return errors.New("fail")
	}, retry.MaxRetries(5), retry.Backoff(1.5), retry.Delay(10*time.Millisecond))
	took := time.Since(start)
	require.Error(t, err)
	assert.Equal(t, 5, attempts, "Expected exactly 5 attempts")
	expected := (10.0 * 1.5 * 1.5 * 1.5 * 1.5 * 1.5) * float64(time.Millisecond)
	assert.True(t, float64(took) > expected, "Expected at least %s", expected)
}

func TestRetryDoFor(t *testing.T) {
	var attempts int
	start := time.Now()
	err := retry.DoFor(100*time.Millisecond, func() error {
		attempts++
		return errors.New("fail")
	}, retry.Delay(10*time.Millisecond))
	took := time.Since(start)
	require.Error(t, err)
	assert.True(t, attempts > 1, "Expected at least 2 attempts")
	assert.True(t, took >= 100*time.Millisecond, "Expected at least 100ms")
}

func TestRetryDoForWithContext(t *testing.T) {
	var attempts int
	start := time.Now()
	err := retry.DoForWithContext(50*time.Millisecond, func(ctx context.Context) error {
		attempts++
		<-ctx.Done()
		return retry.ErrAbort
	}, retry.Delay(10*time.Millisecond))
	took := time.Since(start)
	require.ErrorIs(t, err, retry.ErrAbort)
	assert.True(t, attempts == 1, "Expected exactly 1 attempt")
	assert.True(t, took >= 50*time.Millisecond, "Expected at least 50ms")
}
