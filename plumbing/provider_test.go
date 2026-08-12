package plumbing_test

import (
	"errors"
	"sync"
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

// TestGetPreservesRegistrationOrder covers the guarantee that makes registration
// order meaningful: where two factories both match an input, the one registered
// first wins, and looking up a different input beforehand cannot change that.
//
// Get used to move the factory that matched to the front of the list to save
// probes on later lookups, which broke exactly this.
func TestGetPreservesRegistrationOrder(t *testing.T) {
	p := plumbing.NewProvider[string, string](errors.New("no factory available"))

	// Registered first, so it must win for "both".
	p.Register(func(in string) (string, bool) {
		if in == "both" {
			return "first", true
		}

		return "", false
	})
	// Overlaps on "both", and is the only match for "other".
	p.Register(func(in string) (string, bool) {
		if in == "both" || in == "other" {
			return "second", true
		}

		return "", false
	})

	got, err := p.Get("both")
	require.NoError(t, err)
	assert.Equal(t, "first", got)

	// Resolving "other" is answered by the second factory...
	got, err = p.Get("other")
	require.NoError(t, err)
	assert.Equal(t, "second", got)

	// ...which must not have moved it ahead of the first.
	got, err = p.Get("both")
	require.NoError(t, err)
	assert.Equal(t, "first", got, "the second factory answered ahead of the one registered before it")
}

// TestGetIsSafeForConcurrentUse asserts that parallel lookups all observe the
// same registration order. Get holds only a read lock, so factories run
// concurrently; this is the case that matters under -race.
func TestGetIsSafeForConcurrentUse(t *testing.T) {
	p := plumbing.NewProvider[string, string](errors.New("no factory available"))
	p.Register(func(in string) (string, bool) {
		if in == "both" {
			return "first", true
		}

		return "", false
	})
	p.Register(func(in string) (string, bool) {
		if in == "both" || in == "other" {
			return "second", true
		}

		return "", false
	})

	const workers = 64

	type result struct {
		input string
		got   string
		err   error
	}
	results := make([]result, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			// Half look up the input only the second factory matches, which is what
			// used to reorder the shared list out from under everyone else.
			input := "both"
			if i%2 == 0 {
				input = "other"
			}
			got, err := p.Get(input)
			results[i] = result{input: input, got: got, err: err}
		}()
	}
	wg.Wait()

	want := map[string]string{"both": "first", "other": "second"}
	for i, res := range results {
		require.NoErrorf(t, res.err, "worker %d", i)
		assert.Equalf(t, want[res.input], res.got, "worker %d looked up %q", i, res.input)
	}
}

// TestGetLetsASelfExcludingFactoryBeRegisteredFirst covers the property that
// makes a Provider safe to extend: a factory that matches a superset of another's
// inputs but excludes the cases that other one handles gives the same answer
// wherever it sits in the list. This is what os.ResolveLinuxCompat does, and it is
// what lets a caller add factories to an already-built registry.
func TestGetLetsASelfExcludingFactoryBeRegisteredFirst(t *testing.T) {
	// Matches everything except "specific", which it leaves to the factory below.
	selfExcluding := func(in string) (string, bool) {
		if in == "specific" {
			return "", false
		}

		return "general", true
	}
	specific := func(in string) (string, bool) {
		if in == "specific" {
			return "specific", true
		}

		return "", false
	}

	// Registered in either order, the answers are the same.
	for _, tc := range []struct {
		name  string
		order []plumbing.Factory[string, string]
	}{
		{"general first", []plumbing.Factory[string, string]{selfExcluding, specific}},
		{"specific first", []plumbing.Factory[string, string]{specific, selfExcluding}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := plumbing.NewProvider[string, string](errors.New("no factory available"))
			for _, f := range tc.order {
				p.Register(f)
			}

			got, err := p.Get("specific")
			require.NoError(t, err)
			assert.Equal(t, "specific", got, "the general factory answered for an input the specific one handles")

			got, err = p.Get("anything")
			require.NoError(t, err)
			assert.Equal(t, "general", got)
		})
	}
}

// TestRegisterFirstOverridesAnAlreadyRegisteredFactory covers the case
// self-exclusion cannot reach: a caller has to take precedence over a factory
// matching a superset of their inputs, and cannot make it stand down because it is
// not theirs to change. Registering ahead of it is the only lever they have.
func TestRegisterFirstOverridesAnAlreadyRegisteredFactory(t *testing.T) {
	p := plumbing.NewProvider[string, string](errors.New("no factory available"))

	// A catch-all already in the registry, the way os.ResolveLinuxCompat is.
	p.Register(func(string) (string, bool) {
		return "builtin", true
	})

	p.RegisterFirst(func(in string) (string, bool) {
		if in == "mine" {
			return "override", true
		}

		return "", false
	})

	got, err := p.Get("mine")
	require.NoError(t, err)
	assert.Equal(t, "override", got, "the catch-all answered ahead of a factory registered with RegisterFirst")

	// Inputs the caller's factory declines still reach the catch-all.
	got, err = p.Get("anything")
	require.NoError(t, err)
	assert.Equal(t, "builtin", got)
}

// TestRegisterFirstStaysAheadOfLaterRegistrations asserts the ordering holds
// against factories added after the RegisterFirst call, not just before it. A
// registry stays open for registration, so a caller cannot rely on having been
// last to register.
func TestRegisterFirstStaysAheadOfLaterRegistrations(t *testing.T) {
	p := plumbing.NewProvider[string, string](errors.New("no factory available"))

	p.RegisterFirst(func(string) (string, bool) {
		return "first", true
	})
	p.Register(func(string) (string, bool) {
		return "later", true
	})

	got, err := p.Get("anything")
	require.NoError(t, err)
	assert.Equal(t, "first", got)
}

// TestRegisterFirstPutsTheLatestCallInFront pins what happens when more than one
// caller asks for precedence: each call goes to the very front, so the most recent
// one wins rather than being queued behind the earlier ones.
func TestRegisterFirstPutsTheLatestCallInFront(t *testing.T) {
	p := plumbing.NewProvider[string, string](errors.New("no factory available"))

	p.Register(func(string) (string, bool) {
		return "registered", true
	})
	p.RegisterFirst(func(string) (string, bool) {
		return "earlier", true
	})
	p.RegisterFirst(func(string) (string, bool) {
		return "latest", true
	})

	got, err := p.Get("anything")
	require.NoError(t, err)
	assert.Equal(t, "latest", got, "an earlier RegisterFirst call answered ahead of the most recent one")

	all, err := p.GetAll("anything")
	require.NoError(t, err)
	assert.Equal(t, []string{"latest", "earlier", "registered"}, all)
}

// TestRegisterFirstIsSafeDuringLookups registers while lookups are in flight,
// which is the case that matters under -race: RegisterFirst inserts into the same
// slice Get reads.
func TestRegisterFirstIsSafeDuringLookups(t *testing.T) {
	p := plumbing.NewProvider[string, string](errors.New("no factory available"))
	p.Register(func(string) (string, bool) {
		return "builtin", true
	})

	const workers = 32

	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for range workers {
		go func() {
			defer wg.Done()
			p.RegisterFirst(func(in string) (string, bool) {
				if in == "mine" {
					return "override", true
				}

				return "", false
			})
		}()
		go func() {
			defer wg.Done()
			// Declined by every factory added above, so this is answered by the
			// built-in however many of them have landed by now.
			got, err := p.Get("anything")
			assert.NoError(t, err)
			assert.Equal(t, "builtin", got)
		}()
	}
	wg.Wait()

	got, err := p.Get("mine")
	require.NoError(t, err)
	assert.Equal(t, "override", got)
}
