# Testing with rigtest

The `rigtest` package lets you unit-test code that talks to hosts without a real host.
You program canned responses for the commands your code will run and you can then assert
on what was executed.

## Mocks

There are two, and the choice depends on what your code holds a reference to:

- **`MockRunner`** (`rigtest.NewMockRunner()`) is a full `cmd.Runner`. Use it to test code
  that runs commands directly, anything that calls `Exec`, `ExecOutput`, `ExecContext`,
  `Proc`, etc. on a runner.
- **`MockConnection`** (`rigtest.NewMockConnection()`) is a `protocol.Connection`. Use it
  when your code needs a real `*rig.Client`, for example if you want to drive `client.FS()`,
  `client.OS()`, `client.Sudo()`, or `client.Service()`. Hand it to `NewClient`:

  ```go
  mc := rigtest.NewMockConnection()
  mc.AddCommandOutput(rigtest.Equal("uname -a"), "Linux node-01 6.1.0 x86_64\n")

  client, err := rig.NewClient(rig.WithConnection(mc))
  require.NoError(t, err)
  require.NoError(t, client.Connect(context.Background()))

  out, err := client.ExecOutput("uname -a")   // "Linux node-01 6.1.0 x86_64\n"
  ```

`MockRunner` embeds a `*MockConnection`, so everything below (programming responses,
assertions) works the same on either; reach for the embedded connection
(`mr.MockConnection`) when an API wants a `protocol.Connection`.

Set `mc.Windows = true` (or `mr.MockConnection.Windows = true`) to make the mock report a
Windows host, so `IsWindows()` gated code paths are exercised.

## Canned command responses

Responses are matcher → behavior pairs. The behaviors:

| Method | Effect when the matcher matches |
|---|---|
| `AddCommandOutput(m, "out")` | writes `out` to stdout, exits 0 |
| `AddCommandSuccess(m)` | exits 0 with no output |
| `AddCommandFailure(m, err)` | fails with `err` |
| `AddCommand(m, handler)` | runs `handler(*A) error` for full control |

The matcher is a `CommandMatcher`:

| Matcher | Matches when the command… |
|---|---|
| `Equal(s)` | equals `s` exactly |
| `Contains(s)` | contains substring `s` |
| `HasPrefix(s)` | starts with `s` |
| `HasSuffix(s)` | ends with `s` |
| `Match(re)` | matches regexp pattern `re` |
| `Not(m)` | does **not** match `m` |

```go
mr := rigtest.NewMockRunner()
mr.AddCommandOutput(rigtest.Equal("hostname"), "node-01\n")
mr.AddCommandFailure(rigtest.Contains("/etc/shadow"), errors.New("permission denied"))
```

For dynamic responses, `AddCommand` hands your handler an `*A` carrying the command line
and the std streams, so you can compute output or inspect stdin:

```go
mr.AddCommand(rigtest.HasPrefix("echo "), func(a *rigtest.A) error {
	fmt.Fprintln(a.Stdout, strings.TrimPrefix(a.Command, "echo "))
	return nil
})
```

Matchers are tried in registration order and the first match wins, so register more
specific matchers before broader ones.

### What happens to unmatched commands

By default, a command that matches no matcher succeeds with empty output. This is
convenient (you only program the commands you care about) but can hide a typo in your
code's command string. To make unmatched commands fail instead, set a default error:

```go
mr.ErrDefault = errors.New("unexpected command")   // unmatched commands now fail
mr.ErrImmediate = true                             // fail at start, before any output
```

## Asserting what ran

The `Received*`/`NotReceived*` helpers take your `*testing.T` and the mock, and fail the
test with a readable message:

```go
rigtest.ReceivedEqual(t, mr, "hostname")
rigtest.ReceivedContains(t, mr, "uname")
rigtest.ReceivedMatch(t, mr, `^systemctl (start|restart) k0s`)
rigtest.ReceivedWithPrefix(t, mr, "sudo ")
rigtest.NotReceivedContains(t, mr, "rm -rf")
```

For ad-hoc checks there are lower-level accessors:

```go
mr.Commands()      // []string of every command, in order
mr.LastCommand()   // most recent command
mr.Len()           // how many ran
mr.Reset()         // clear recorded commands and matchers between sub-tests
mr.Received(rigtest.Equal("hostname"))   // returns error (nil if matched) — the primitive behind the helpers
```

## Capturing rig's log output

`MockLogger` records structured log lines so you can assert on them. It satisfies rig's
`log.Logger`, so inject it with `rig.WithLogger`:

```go
ml := &rigtest.MockLogger{}
client, _ := rig.NewClient(rig.WithConnection(mc), rig.WithLogger(ml))
// ... exercise client ...
require.True(t, ml.ReceivedSubstring("connected"))
// also: ml.Received(regexp), ml.ReceivedString(exact), ml.Messages(), ml.Len(), ml.Reset()
```

## Debugging a mock that won't match

When a test fails because a command didn't match what you programmed, surface the exact
command strings rig issued. `Trace` runs a function with rig's internal trace logging on;
`TraceToStderr()` / `TraceOff()` toggle it globally:

```go
rigtest.Trace(func() {
	out, err := client.OS()   // trace logs every command rig runs during detection
	_ = out
	_ = err
})
```

This is the fastest way to discover, for example, that OS detection runs `cat /etc/os-release`
rather than whatever you guessed.

## Match the real command, not the method

Mocks match on the command string that actually runs on the host, not on the Go
method you called. When you test code that goes through a provider — `client.FS()`,
`client.OS()`, `client.Service()` create the matcher for the underlying command, not
the method name. For example, `client.FS().SystemTime()` runs `date -u +%s` on the host, so
the mock is keyed on that:

```go
mr.AddCommandOutput(rigtest.Contains("date -u +%s"), "1700000000")
```

If you're not sure what command a provider emits, use the `Trace` helper above or look at the
source to find out.
