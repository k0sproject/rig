package sshconfig

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// This file defines a parser that converts ssh configuration files into a tree structure.
//
// This parsing has to be usually done just once and the same parsed tree is then used
// to apply settings to individual ssh host configurations.
//
// It is not exported as there should be very little use for it outside of the actual
// config parser.

// node represents a single node in the ssh config tree.
type node struct {
	key      string   // node key
	values   []string // node values (each row can have multiple values)
	children []*node  // child nodes
	path     string   // file path of the config file
	row      int      // row number in the config file
}

// newNode creates a new [Node].
func newNode(key string, values []string, path string, row int) *node {
	return &node{key: key, values: values, path: path, row: row}
}

// Key of the node.
func (d *node) Key() string {
	return d.key
}

// Values of the node.
func (d *node) Values() []string {
	return d.values
}

// Path of the node, which is the file path of the config file.
func (d *node) Path() string {
	return d.path
}

// Row of the node, which is the row number in the config file.
func (d *node) Row() int {
	return d.row
}

// Children of the node.
func (d *node) Children() []*node {
	return d.children
}

// AddChild adds a child to the node.
func (d *node) AddChild(child *node) {
	d.children = append(d.children, child)
}

// nodeIterator is an iterator for the children of a node.
type nodeIterator struct {
	node    *node
	currIdx int
}

// newNodeIterator creates a new [NodeIterator].
func newNodeIterator(root *node) *nodeIterator {
	return &nodeIterator{node: root, currIdx: -1}
}

// NextChild returns the next child of the node.
func (ni *nodeIterator) NextChild() *node {
	ni.currIdx++
	if ni.currIdx < len(ni.node.children) {
		return ni.node.children[ni.currIdx]
	}
	return nil
}

// treeIterator is an iterator for the ssh config tree.
type treeIterator struct {
	root  *node
	stack []*nodeIterator
}

// newTreeIterator creates a new [TreeIterator].
func newTreeIterator(root *node) *treeIterator {
	return &treeIterator{root: root, stack: []*nodeIterator{newNodeIterator(root)}}
}

// Key of the current node.
func (t *treeIterator) Key() string {
	if len(t.stack) == 0 {
		return ""
	}
	return t.stack[len(t.stack)-1].node.key
}

// Values of the current node.
func (t *treeIterator) Values() []string {
	if len(t.stack) == 0 {
		return nil
	}
	return t.stack[len(t.stack)-1].node.values
}

// Path of the current node.
func (t *treeIterator) Path() string {
	if len(t.stack) == 0 {
		return ""
	}
	return t.stack[len(t.stack)-1].node.path
}

// Row of the current node.
func (t *treeIterator) Row() int {
	if len(t.stack) == 0 {
		return 0
	}
	return t.stack[len(t.stack)-1].node.row
}

// Depth of the current node.
func (t *treeIterator) Depth() int {
	return len(t.stack)
}

// Skip the children of the current node.
func (t *treeIterator) Skip() {
	t.stack[len(t.stack)-1].currIdx = len(t.stack[len(t.stack)-1].node.children)
}

// Next moves to the next node.
func (t *treeIterator) Next() bool {
	for len(t.stack) > 0 {
		topIterator := t.stack[len(t.stack)-1]
		nextChild := topIterator.NextChild()
		if nextChild == nil {
			// Pop the exhausted iterator
			t.stack = t.stack[:len(t.stack)-1]
		} else {
			// Push an iterator for the next child onto the stack
			t.stack = append(t.stack, newNodeIterator(nextChild))
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
func (t *treeIterator) Reset() {
	t.stack = []*nodeIterator{newNodeIterator(t.root)}
}

// treeParser parses ssh config files into a tree structure.
type treeParser struct {
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
	if home := userhome(); home != "" {
		return filepath.Join(home, ".ssh", "config")
	}
	return filepath.Join(".", ".ssh", "config")
}

// newTreeParser creates a new [TreeParser].
func newTreeParser(input io.Reader) *treeParser {
	return &treeParser{r: input, GlobalConfigPath: defaultGlobalConfigPath(), UserConfigPath: defaultUserConfigPath()}
}

func (p *treeParser) setupDirs() {
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
func (p *treeParser) Parse() (*treeIterator, error) { //nolint:cyclop
	p.setupDirs()

	root := newNode("root", nil, "", 0)
	if p.r != nil { //nolint:nestif
		var readerPath string
		if nr, ok := p.r.(withName); ok {
			readerPath = nr.Name()
		} else {
			readerPath = "./unknown"
		}
		if prc, ok := p.r.(io.Closer); ok {
			defer prc.Close()
		}
		customConfig := newNode("custom", nil, readerPath, 0)
		root.AddChild(customConfig)
		if err := p.parseTree(p.r, customConfig, make(map[string]struct{})); err != nil {
			return nil, fmt.Errorf("failed to parse supplied ssh config: %w", err)
		}
	} else {
		var userReader io.Reader
		var err error
		if p.UserConfigReader != nil {
			userReader = p.UserConfigReader
		} else {
			userReader, err = os.Open(p.UserConfigPath)
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to open user ssh config %q: %w", p.UserConfigPath, err)
			}
		}
		if ucc, ok := userReader.(io.Closer); ok {
			defer ucc.Close()
		}
		if err == nil {
			userConfig := newNode("custom", nil, p.UserConfigPath, 0)
			root.AddChild(userConfig)
			if err := p.parseTree(userReader, userConfig, make(map[string]struct{})); err != nil {
				return nil, fmt.Errorf("failed to parse user ssh config %q: %w", p.UserConfigPath, err)
			}
		}
		var globalReader io.Reader
		err = nil
		if p.GlobalConfigReader != nil {
			globalReader = p.GlobalConfigReader
		} else {
			globalReader, err = os.Open(p.GlobalConfigPath)
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to open global ssh config %q: %w", p.GlobalConfigPath, err)
			}
		}
		if gcc, ok := globalReader.(io.Closer); ok {
			defer gcc.Close()
		}
		if err == nil {
			globalConfig := newNode("global", nil, p.GlobalConfigPath, 0)
			root.AddChild(globalConfig)
			if err := p.parseTree(globalReader, globalConfig, make(map[string]struct{})); err != nil {
				return nil, fmt.Errorf("failed to parse global ssh config %q: %w", p.GlobalConfigPath, err)
			}
		}
	}

	dc := strings.NewReader(sshDefaultConfig)
	defaultConfig := newNode("default", nil, "__default__", 0)
	root.AddChild(defaultConfig)
	if err := p.parseTree(dc, defaultConfig, make(map[string]struct{})); err != nil {
		return nil, fmt.Errorf("failed to parse default ssh config: %w", err)
	}

	// finally set a default username
	if u := username(); u != "" {
		root.AddChild(newNode("user", []string{u}, "default", 0))
	}

	return newTreeIterator(root), nil
}

// tokenizeRow splits a line into a key and a value.
// any comments are stripped from the value.
//
// The OpenSSH client does this by testing the list of known keys against
// the position of the first non-space character of the line, so it doesn't
// actually even look for a separator.
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
		return "", nil, fmt.Errorf("%w: missing separator: %q", ErrSyntax, s)
	}

	key = strings.ToLower(leftTrimmed[:idx])
	if len(leftTrimmed) < idx+1 {
		return "", nil, fmt.Errorf("%w: missing value: %q", ErrSyntax, s)
	}

	leftTrimmed = leftTrimmed[idx+1:]

	idx = strings.IndexFunc(leftTrimmed, func(r rune) bool { return !unicode.IsSpace(r) && r != '=' })
	breakOnComment := true
	switch key {
	case "knownhostscommand", "proxycommand", "localcommand", "remotecommand":
		// the comment is a part of the value for commands
		breakOnComment = false
	}
	values, err = splitArgs(leftTrimmed[idx:], breakOnComment)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrSyntax, err)
	}

	if len(values) == 0 {
		return "", nil, fmt.Errorf("%w: missing value: %q", ErrSyntax, s)
	}

	return key, values, nil
}

func (p *treeParser) parseTree(reader io.Reader, root *node, includes map[string]struct{}) error { //nolint:cyclop
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
			newNode := newNode(key, values, currNode.Path(), row)
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
					childDirective := newNode("include", values, match, row)
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
			currNode.AddChild(newNode(key, values, currNode.Path(), row))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input from %q: %w", currNode.Path(), err)
	}

	return nil
}
