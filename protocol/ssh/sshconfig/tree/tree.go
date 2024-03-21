package tree

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/k0sproject/rig/v2/homedir"
)

var (
	// ErrSyntax is returned when the config file has a syntax error.
	ErrSyntax = errors.New("syntax error")
)

// Node represents a single node in the ssh config tree.
type Node struct {
	key      string   // node key
	values   []string // node values (each row can have multiple values)
	children []*Node  // child nodes
	path     string   // file path of the config file
	row      int      // row number in the config file
}

// NewNode creates a new [Node].
func NewNode(key string, values []string, path string, row int) *Node {
	return &Node{key: key, values: values, path: path, row: row}
}

// Key of the node.
func (d *Node) Key() string {
	return d.key
}

// Values of the node.
func (d *Node) Values() []string {
	return d.values
}

// Path of the node, which is the file path of the config file.
func (d *Node) Path() string {
	return d.path
}

// Row of the node, which is the row number in the config file.
func (d *Node) Row() int {
	return d.row
}

// Children of the node.
func (d *Node) Children() []*Node {
	return d.children
}

// AddChild adds a child to the node.
func (d *Node) AddChild(child *Node) {
	d.children = append(d.children, child)
}

// NodeIterator is an iterator for the children of a node.
type NodeIterator struct {
	node    *Node
	currIdx int
}

// NewNodeIterator creates a new [NodeIterator].
func NewNodeIterator(node *Node) *NodeIterator {
	return &NodeIterator{node: node, currIdx: -1}
}

// NextChild returns the next child of the node.
func (ni *NodeIterator) NextChild() *Node {
	ni.currIdx++
	if ni.currIdx < len(ni.node.children) {
		return ni.node.children[ni.currIdx]
	}
	return nil
}

// TreeIterator is an iterator for the ssh config tree.
type TreeIterator struct {
	root  *Node
	stack []*NodeIterator
}

// NewTreeIterator creates a new [TreeIterator].
func NewTreeIterator(root *Node) *TreeIterator {
	return &TreeIterator{root: root, stack: []*NodeIterator{NewNodeIterator(root)}}
}

// Key of the current node.
func (t *TreeIterator) Key() string {
	if len(t.stack) == 0 {
		return ""
	}
	return t.stack[len(t.stack)-1].node.key
}

// Values of the current node.
func (t *TreeIterator) Values() []string {
	if len(t.stack) == 0 {
		return nil
	}
	return t.stack[len(t.stack)-1].node.values
}

// Path of the current node.
func (t *TreeIterator) Path() string {
	if len(t.stack) == 0 {
		return ""
	}
	return t.stack[len(t.stack)-1].node.path
}

// Row of the current node.
func (t *TreeIterator) Row() int {
	if len(t.stack) == 0 {
		return 0
	}
	return t.stack[len(t.stack)-1].node.row
}

// Depth of the current node.
func (t *TreeIterator) Depth() int {
	return len(t.stack)
}

// Skip the children of the current node.
func (t *TreeIterator) Skip() {
	t.stack[len(t.stack)-1].currIdx = len(t.stack[len(t.stack)-1].node.children)
}

// Next moves to the next node.
func (t *TreeIterator) Next() bool {
	for len(t.stack) > 0 {
		topIterator := t.stack[len(t.stack)-1]
		nextChild := topIterator.NextChild()
		if nextChild == nil {
			// Pop the exhausted iterator
			t.stack = t.stack[:len(t.stack)-1]
		} else {
			// Push an iterator for the next child onto the stack
			t.stack = append(t.stack, NewNodeIterator(nextChild))
			if t.stack[len(t.stack)-1].node.values == nil {
				// skip nodes without values (these are added by the parser for grouping)
				return t.Next()
			}
			return true
		}
	}
	return false
}

// Reset the iterator to the root node.
func (t *TreeIterator) Reset() {
	t.stack = []*NodeIterator{NewNodeIterator(t.root)}
}

// TreeParser parses ssh config files into a tree structure.
type TreeParser struct {
	r io.Reader

	// GlobalConfigPath is the path to the global ssh config file, the default is /etc/ssh/ssh_config and PROGRAMDATA/ssh/ssh_config on windows.
	GlobalConfigPath string
	// UserConfigPath is the path to the user ssh config file, the default is ~/.ssh/config.
	UserConfigPath string
	// GlobalConfigReader is the reader for the global ssh config file. This is used for testing.
	GlobalConfigReader io.Reader
	// UserConfigReader is the reader for the user ssh config file. This is used for testing.
	UserConfigReader io.Reader

	globalConfigDir string
	userConfigDir   string
}

func defaultUserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// NewTreeParser creates a new [TreeParser].
func NewTreeParser(input io.Reader) *TreeParser {
	return &TreeParser{r: input, GlobalConfigPath: defaultGlobalConfigPath(), UserConfigPath: defaultUserConfigPath()}
}

func (p *TreeParser) setupDirs() {
	if p.r != nil {
		return
	}
	if p.userConfigDir == "" {
		p.userConfigDir = filepath.Dir(p.UserConfigPath)
	}
	if p.userConfigDir == "" && p.UserConfigReader != nil {
		if nr, ok := p.UserConfigReader.(withName); ok {
			p.userConfigDir = filepath.Dir(nr.Name())
		}
	}
	if p.globalConfigDir == "" {
		p.globalConfigDir = filepath.Dir(p.GlobalConfigPath)
	}
	if p.globalConfigDir == "" && p.GlobalConfigReader != nil {
		if nr, ok := p.GlobalConfigReader.(withName); ok {
			p.globalConfigDir = filepath.Dir(nr.Name())
		}
	}
}

type withName interface {
	Name() string
}

// Parse the ssh config files into a tree structure and return a [TreeIterator].
func (p *TreeParser) Parse() (*TreeIterator, error) {
	p.setupDirs()

	root := NewNode("root", nil, "", 0)
	if p.r != nil { //nolint:nestif
		var readerPath string
		if nr, ok := p.r.(withName); ok {
			readerPath = nr.Name()
		} else {
			readerPath, _ = homedir.Expand("~/.ssh/unknown")
		}
		if prc, ok := p.r.(io.Closer); ok {
			defer prc.Close()
		}
		customConfig := NewNode("custom", nil, readerPath, 0)
		root.AddChild(customConfig)
		if err := p.parseTree(p.r, customConfig, make(map[string]struct{})); err != nil {
			return nil, fmt.Errorf("failed to parse supplied ssh config: %w", err)
		}
	} else {
		var uc io.Reader
		var err error
		if p.UserConfigReader != nil {
			uc = p.UserConfigReader
		} else {
			uc, err = os.Open(p.UserConfigPath)
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to open user ssh config %q: %w", p.UserConfigPath, err)
			}
		}
		if ucc, ok := uc.(io.Closer); ok {
			defer ucc.Close()
		}
		if err == nil {
			userConfig := NewNode("custom", nil, p.UserConfigPath, 0)
			root.AddChild(userConfig)
			if err := p.parseTree(uc, userConfig, make(map[string]struct{})); err != nil {
				return nil, fmt.Errorf("failed to parse user ssh config %q: %w", p.UserConfigPath, err)
			}
		}
		var gc io.Reader
		err = nil
		if p.GlobalConfigReader != nil {
			gc = p.GlobalConfigReader
		} else {
			gc, err = os.Open(p.GlobalConfigPath)
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to open global ssh config %q: %w", p.GlobalConfigPath, err)
			}
		}
		if err == nil {
			globalConfig := NewNode("global", nil, p.GlobalConfigPath, 0)
			root.AddChild(globalConfig)
			if err := p.parseTree(gc, globalConfig, make(map[string]struct{})); err != nil {
				return nil, fmt.Errorf("failed to parse global ssh config %q: %w", p.GlobalConfigPath, err)
			}
		}
	}

	dc := strings.NewReader(sshDefaultConfig)
	defaultConfig := NewNode("default", nil, "default", 0)
	root.AddChild(defaultConfig)
	if err := p.parseTree(dc, defaultConfig, make(map[string]struct{})); err != nil {
		return nil, fmt.Errorf("failed to parse default ssh config: %w", err)
	}
	if u, err := user.Current(); err == nil {
		// finally set a default username
		root.AddChild(NewNode("user", []string{u.Username}, "default", 0))
	}

	return NewTreeIterator(root), nil
}

// tokenizeRow splits a line into a key and a value.
// any comments are stripped from the value.
//
// note that comments are parsed in a different way
// depending on if the key and value are separated with
// a space or an equals sign.
//
// for example here the comment becomes part of the
// value:
//
// IdentityFile ~/.ssh/id_rsa # foo
//
// but here it doesn't:
//
// IdentityFile=~/.ssh/id_rsa # foo.
func tokenizeRow(s string) (key string, values []string, err error) {
	// find the first non-space character
	idx := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsSpace(r) })

	// skip comments
	if idx == -1 || s[idx] == '#' {
		return "", nil, nil
	}

	leftTrimmed := s[idx:]

	// find separator
	idx = strings.IndexFunc(leftTrimmed, func(r rune) bool { return r == '=' || unicode.IsSpace(r) })

	// if there is no separator, the line is invalid
	if idx == -1 {
		if strings.HasPrefix(leftTrimmed, "canonicaldomains") {
			// some versions of ssh output a broken line for canonicaldomains that doesn't include a value
			return "canonicaldomains", []string{"none"}, nil
		}
		return "", nil, fmt.Errorf("%w: missing separator and value: %q", ErrSyntax, s)
	}

	key = strings.ToLower(leftTrimmed[:idx])
	if len(leftTrimmed) < idx+1 {
		return "", nil, fmt.Errorf("%w: missing value: %q", ErrSyntax, s)
	}

	leftTrimmed = leftTrimmed[idx+1:]

	idx = strings.IndexFunc(leftTrimmed, func(r rune) bool { return !unicode.IsSpace(r) && r != '=' })
	values, err = splitArgs(leftTrimmed[idx:], true)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrSyntax, err)
	}

	if len(values) == 0 {
		return "", nil, fmt.Errorf("%w: missing value: %q", ErrSyntax, s)
	}

	return key, values, nil
}

func (p *TreeParser) parseTree(reader io.Reader, root *Node, includes map[string]struct{}) error { //nolint:gocognit,cyclop
	scanner := bufio.NewScanner(reader)
	currNode := root
	var row int
	for scanner.Scan() {
		row++
		key, values, err := tokenizeRow(scanner.Text())
		if err != nil {
			return fmt.Errorf("%w: parse error on row %d: %w", ErrSyntax, row, err)
		}
		if key == "" {
			continue
		}
		switch key {
		case "host", "match":
			newNode := NewNode(key, values, currNode.Path(), row)
			root.AddChild(newNode)
			currNode = newNode
		case "include":
			for _, value := range values {
				value = filepath.Clean(value)
				if slices.Contains(strings.Split(value, string(os.PathSeparator)), "..") {
					return fmt.Errorf("%w: parse error on row %d: include directive contains a path traversing relative path", ErrSyntax, row)
				}
				if !filepath.IsAbs(value) {
					if strings.HasPrefix(currNode.Path(), p.globalConfigDir) {
						value = filepath.Join(p.globalConfigDir, value)
					} else {
						value = filepath.Join(p.userConfigDir, value)
					}
				}
				if _, ok := includes[value]; ok {
					return fmt.Errorf("%w: parse error on row %d: circular include directive", ErrSyntax, row)
				}
				matches, err := filepath.Glob(value)
				if err != nil {
					return fmt.Errorf("can't glob include path %q: %w", value, err)
				}
				sort.Strings(matches)
				for _, match := range matches {
					f, err := os.Open(match) //nolint:varnamelen
					if err != nil {
						return fmt.Errorf("can't open include path %q: %w", match, err)
					}
					newIncludes := make(map[string]struct{})
					for k := range includes {
						newIncludes[k] = struct{}{}
					}
					newIncludes[match] = struct{}{}
					childDirective := NewNode("include", values, match, row)
					currNode.AddChild(childDirective)
					err = p.parseTree(f, childDirective, newIncludes)
					f.Close()
					if err != nil {
						return fmt.Errorf("failed to parse include file %q in %s:%d: %w", match, currNode.Path(), row, err)
					}
					f.Close()
				}
			}
		default:
			currNode.AddChild(NewNode(key, values, currNode.Path(), row))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input from %q: %w", currNode.Path(), err)
	}

	return nil
}
