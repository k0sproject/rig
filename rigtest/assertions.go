package rigtest

import "fmt"

// TestingT is an interface that is compatible with the testing.T.
type TestingT interface {
	Errorf(format string, args ...interface{})
}

type tHelper interface {
	Helper()
}

// Receiver is any object in rigtest that expects to receive commands.
type Receiver interface {
	Received(matchFn CommandMatcher) error
	NotReceived(matchFn CommandMatcher) error
}

func logExtraMsg(t TestingT, msgAndArgs ...any) { //nolint:varnamelen
	if len(msgAndArgs) == 0 {
		return
	}
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if len(msgAndArgs) == 1 {
		if s, ok := msgAndArgs[0].(string); ok {
			t.Errorf(s)
		} else {
			t.Errorf("%v", msgAndArgs[0])
		}
		return
	}

	if s, ok := msgAndArgs[0].(string); ok {
		t.Errorf(s, msgAndArgs[1:]...)
		return
	}
	t.Errorf(fmt.Sprint(msgAndArgs...))
}

// ReceivedEqual asserts that a command was received.
func ReceivedEqual(t TestingT, m Receiver, command string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.Received(Equal(command)); err != nil {
		t.Errorf("Expected to have received command `%s`: %v", command, err)
		logExtraMsg(t, msgAndArgs...)
	}
}

// ReceivedWithPrefix asserts that a command with the given prefix was received.
func ReceivedWithPrefix(t TestingT, m Receiver, prefix string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.Received(HasPrefix(prefix)); err != nil {
		t.Errorf("Expected to have received a command starting with `%s`:  %v", prefix, err)
		logExtraMsg(t, msgAndArgs...)
	}
}

// ReceivedWithSuffix asserts that a command with the given suffix was received.
func ReceivedWithSuffix(t TestingT, m Receiver, suffix string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.Received(HasPrefix(suffix)); err != nil {
		t.Errorf("Expected to have received a command ending with `%s`: %v", suffix, err)
		logExtraMsg(t, msgAndArgs...)
	}
}

// ReceivedContains asserts that a command with the given substring was received.
func ReceivedContains(t TestingT, m Receiver, substring string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.Received(Contains(substring)); err != nil {
		t.Errorf("Expected to have received a command with substring `%s`:  %v", substring, err)
		logExtraMsg(t, msgAndArgs...)
	}
}

// ReceivedMatch asserts that a command with the given regular expression was received.
func ReceivedMatch(t TestingT, m Receiver, pattern string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.Received(Match(pattern)); err != nil {
		t.Errorf("Expected to have received a command matching pattern `%s`: %v", pattern, err)
		logExtraMsg(t, msgAndArgs...)
	}
}

// NotReceivedEqual asserts that a command was not received.
func NotReceivedEqual(t TestingT, m Receiver, command string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.NotReceived(Equal(command)); err != nil {
		t.Errorf("Expected to not have received command `%s` but did.", command)
		logExtraMsg(t, msgAndArgs...)
	}
}

// NotReceivedWithPrefix asserts that a command with the given prefix was not received.
func NotReceivedWithPrefix(t TestingT, m Receiver, prefix string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.NotReceived(HasPrefix(prefix)); err != nil {
		t.Errorf("Expected to not have received a command starting with `%s` but did.", prefix)
		logExtraMsg(t, msgAndArgs...)
	}
}

// NotReceivedWithSuffix asserts that a command with the given suffix was not received.
func NotReceivedWithSuffix(t TestingT, m Receiver, suffix string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.NotReceived(HasSuffix(suffix)); err != nil {
		t.Errorf("Expected to not have received a command ending with `%s` but did.", suffix)
		logExtraMsg(t, msgAndArgs...)
	}
}

// NotReceivedContains asserts that a command with the given substring was not received.
func NotReceivedContains(t TestingT, m Receiver, substring string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.NotReceived(Contains(substring)); err != nil {
		t.Errorf("Expected to not have received a command with substring `%s` but did.", substring)
		logExtraMsg(t, msgAndArgs...)
	}
}

// NotReceivedMatch asserts that a command with the given regular expression was not received.
func NotReceivedMatch(t TestingT, m Receiver, pattern string, msgAndArgs ...any) {
	if h, ok := t.(tHelper); ok {
		h.Helper()
	}
	if err := m.NotReceived(Match(pattern)); err != nil {
		t.Errorf("Expected to not have received a command matching pattern `%s` but did.", pattern)
		logExtraMsg(t, msgAndArgs...)
	}
}
