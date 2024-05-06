package shellescape

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var (
	errInvalidInput   = errors.New("invalid input")
	errNotImplemented = errors.New("not implemented")
)

type expandOptions struct {
	dollarvars bool
	errorunset bool
	params     bool
	exec       bool
	// TODO execbacktick
}

func newExpandOptions(options ...ExpandOption) *expandOptions {
	opts := &expandOptions{dollarvars: true}
	for _, o := range options {
		o(opts)
	}
	return opts
}

// ExpandOption is a functional option for Expand.
type ExpandOption func(*expandOptions)

// ExpandExec enables command substitution, as in $(command).
func ExpandExec() ExpandOption {
	return func(o *expandOptions) {
		o.exec = true
	}
}

// ExpandParam enables parameter expansion, as in ${parameter:...} and some other patterns. If this is not set,
// only simple ${VAR} expansion is performed.
func ExpandParam() ExpandOption {
	return func(o *expandOptions) {
		o.params = true
	}
}

// ExpandErrorIfUnset causes Expand to return an error if a variable is not set. By default, unset variables are
// replaced with an empty string. This only applies when ExpandParam is not set.
func ExpandErrorIfUnset() ExpandOption {
	return func(o *expandOptions) {
		o.errorunset = true
	}
}

// ExpandNoDollarVars disables $var expansion.
func ExpandNoDollarVars() ExpandOption {
	return func(o *expandOptions) {
		o.dollarvars = false
	}
}

type builderStack []*strings.Builder

func (s *builderStack) push() {
	*s = append(*s, &strings.Builder{})
}

func (s *builderStack) pop() {
	if len(*s) == 0 {
		return
	}
	(*s)[len(*s)-1].Reset()
	*s = append((*s)[:len(*s)-1], (*s)[len(*s):]...)
}

func (s *builderStack) top() *strings.Builder {
	if len(*s) == 0 {
		s.push()
	}
	return (*s)[len(*s)-1]
}

func (s *builderStack) String() string {
	str := s.top().String()
	s.pop()
	return str
}

func (s *builderStack) WriteByte(b byte) error {
	if err := s.top().WriteByte(b); err != nil {
		return fmt.Errorf("builder stack: %w", err)
	}
	return nil
}

func (s *builderStack) WriteString(str string) (int, error) {
	n, err := s.top().WriteString(str)
	if err != nil {
		return n, fmt.Errorf("builder stack: %w", err)
	}
	return n, nil
}

func (s *builderStack) Write(p []byte) (int, error) {
	n, err := s.top().Write(p)
	if err != nil {
		return n, fmt.Errorf("builder stack: %w", err)
	}
	return n, nil
}

func (s *builderStack) WriteRune(r rune) (int, error) {
	n, err := s.top().WriteRune(r)
	if err != nil {
		return n, fmt.Errorf("builder stack: %w", err)
	}
	return n, nil
}

func (s *builderStack) Len() int {
	return s.top().Len()
}

func (s *builderStack) Size() int {
	return len(*s)
}

func (s *builderStack) Dump() {
	for i, b := range *s {
		fmt.Printf("%s- stack[%d]: %q\n", strings.Repeat(" ", i), i, b.String())
	}
}

func getEnv(varName string, options *expandOptions) (string, error) {
	val, ok := os.LookupEnv(varName)
	if !ok && options.errorunset {
		return "", fmt.Errorf("%w: expand: variable %q not set", errInvalidInput, varName)
	}
	return val, nil
}

// Expand expands the input string according to the rules of a posix shell. It supports parameter expansion, command
// substitution and simple $envvar expansion. It does not support arithmetic expansion, tilde expansion, or any of
// the other expansions and it doesn't support backticks.
func Expand(input string, opts ...ExpandOption) (string, error) { //nolint:cyclop,funlen
	options := newExpandOptions(opts...)
	var inDollar, inCurly, inParen, inEscape bool
	stack := builderStack{}
	openParen := 0

	for i := range len(input) {
		currCh := input[i]

		if inEscape {
			_ = stack.WriteByte(currCh)
			inEscape = false
			continue
		}

		if currCh == '\\' {
			inEscape = true
			continue
		}

		if !inDollar && !inCurly && !inEscape && currCh == '$' {
			inDollar = true
			stack.push()
			continue
		}

		if inDollar { //nolint:nestif
			if currCh == '$' {
				inDollar = false
				_ = stack.WriteByte(currCh)
				continue
			}
			if currCh == '{' {
				inDollar = false
				inCurly = true
				continue
			}
			if currCh == '(' && options.exec {
				inDollar = false
				inParen = true
				openParen++
				continue
			}

			if isValidVarNameChar(currCh, stack.Len() == 0) {
				_ = stack.WriteByte(currCh)
				continue
			}

			// Finish processing variable
			inDollar = false
			var val string
			varName := stack.String()
			if options.dollarvars {
				v, err := getEnv(varName, options)
				if err != nil {
					return "", err
				}
				val = v
			} else {
				val = "$" + varName
			}
			_, _ = stack.WriteString(val)
			_ = stack.WriteByte(currCh)
			continue
		}

		if inCurly { //nolint:nestif
			if currCh == '}' {
				inCurly = false
				var result string
				if options.params {
					res, err := expandCurly(stack.String())
					if err != nil {
						return "", err
					}
					result = res
				} else {
					val, err := getEnv(stack.String(), options)
					if err != nil {
						return "", err
					}
					result = val
				}
				_, _ = stack.WriteString(result)
				continue
			}
			_ = stack.WriteByte(currCh)
			continue
		}

		if inParen && currCh == ')' {
			openParen--
			if openParen == 0 {
				inParen = false
			}
			res, err := evalCmd(stack.String())
			if err != nil {
				return "", err
			}
			_, _ = stack.WriteString(res)
			continue
		}

		_ = stack.WriteByte(currCh)
	}

	for stack.Size() > 1 {
		_, _ = stack.WriteString(stack.String())
	}

	if inCurly || inParen {
		return "", fmt.Errorf("%w: expand: unclosed ${ or $( in %q", errInvalidInput, input)
	}

	return stack.String(), nil
}

func isValidVarNameChar(ch byte, isFirstChar bool) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch == '_' || (!isFirstChar && ch >= '0' && ch <= '9')
}

func evalCmd(input string) (string, error) {
	if input == "" {
		return "", nil
	}
	parts, err := Split(input)
	if err != nil {
		return "", fmt.Errorf("eval: split %q: %w", input, err)
	}
	cmd := exec.Command(parts[0], parts[1:]...) //nolint:gosec
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("eval %q: %w", input, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

func expandOffsetLength(val, offsetStr, lengthStr string) (string, error) {
	offset, err := strconv.Atoi(strings.TrimSpace(offsetStr))
	if err != nil {
		return "", fmt.Errorf("%w: bad substitution: offset: %w", errInvalidInput, err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(lengthStr))
	if err != nil {
		return "", fmt.Errorf("%w: bad substitution: length: %w", errInvalidInput, err)
	}
	if val == "" {
		return "", nil
	}
	if offset < 0 {
		if offset*-1 > len(val) {
			return "", nil
		}
		offset = len(val) + offset
		if offset+length < 0 {
			return "", nil
		}
	}
	if offset < 0 {
		length += offset
		offset = 0
	}
	if offset > len(val) {
		return "", nil
	}
	return val[offset : offset+length], nil
}

func expandOffset(val, offsetStr string) (string, error) {
	offset, err := strconv.Atoi(strings.TrimSpace(offsetStr))
	if err != nil {
		return "", fmt.Errorf("%w: bad substitution: offset: %w", errInvalidInput, err)
	}
	if val == "" {
		return "", nil
	}
	if offset < 0 {
		if offset*-1 > len(val) {
			return "", nil
		}
		offset = len(val) + offset
		if offset < 0 {
			return "", nil
		}
	}
	return val[offset:], nil
}

func expandColon(val, pattern string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("%w: bad substitution - empty pattern after ':'", errInvalidInput)
	}

	parts := strings.Split(pattern, ":")
	if len(parts) == 2 {
		// ${parameter:offset:length}
		return expandOffsetLength(val, parts[0], parts[1])
	}

	if strings.HasPrefix(pattern, " -") {
		// ${parameter: -offset}
		return expandOffset(val, pattern[1:])
	}

	if pattern[0] >= '0' && pattern[0] <= '9' {
		// ${parameter:offset}
		return expandOffset(val, pattern)
	}

	switch pattern[0] {
	case '-':
		if val == "" {
			return pattern[1:], nil
		}
		return val, nil
	case '+':
		if val == "" {
			return "", nil
		}
		return pattern[1:], nil
	}

	return "", fmt.Errorf("%w: bad substitution (${%s}) - invalid pattern after ':'", errInvalidInput, pattern)
}

func expandVarNames(input string) (string, error) {
	suffix := input[len(input)-1]
	if suffix != '*' && suffix != '@' {
		return "", nil
	}
	input = input[:len(input)-1]
	var varnames []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, input) {
			idx := strings.Index(env, "=")
			if idx == -1 {
				continue
			}
			varnames = append(varnames, env[:idx])
		}
	}
	sep := ' '
	if ifs := os.Getenv("IFS"); ifs != "" {
		sep = rune(ifs[0])
	}
	return strings.Join(varnames, string(sep)), nil
}

func expandCurly(input string) (string, error) {
	if input == "" {
		return "", nil
	}

	if input[0] == '#' {
		// ${#parameter} - The length in characters of its value.
		if len(input) == 1 {
			return "", fmt.Errorf("%w: bad substitution (${%s}) - lone '#'", errInvalidInput, input)
		}
		return strconv.Itoa(len(os.Getenv(input[1:]))), nil
	}

	if input[0] == '!' {
		// ${!prefix*} or ${!prefix@} -- list of var names matching prefix
		return expandVarNames(input[1:])
	}

	idx := -1
	for i := range len(input) {
		idx = i
		if !isValidVarNameChar(input[i], i == 0) {
			break
		}
	}

	if idx == 0 {
		return "", fmt.Errorf("%w: bad substitution (${%s}) - operator at beginning", errInvalidInput, input)
	}

	if idx == len(input)-1 {
		// no separator, just a simple variable
		return os.Getenv(input), nil
	}

	// operator + value must be at least 2 chars after the variable name
	if len(input) < idx+2 {
		return "", fmt.Errorf("%w: bad substitution (${%s}) - no operator after variable name", errInvalidInput, input)
	}

	name := input[:idx]
	val := os.Getenv(name)
	patternCh := input[idx:][0]
	pattern := input[idx:][1:]

	switch patternCh {
	case ':':
		return expandColon(val, pattern)
	default:
		return "", fmt.Errorf("%w: support for pattern ${%s} not implemented", errNotImplemented, input)
	}
}
