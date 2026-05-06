package remotefs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

var (
	// ErrNilPatch is returned by PatchFile when a Patch with a nil apply function is encountered.
	ErrNilPatch = errors.New("patch has nil apply function")
	// ErrInvalidMatch is returned when a zero-value LineMatch (no matcher function set) is used.
	ErrInvalidMatch = errors.New("invalid line match: use ByPrefix, ByExact, ByContains, or ByRegex")
	// ErrMultilinePatch is returned by patch constructors when the line argument contains newline characters.
	ErrMultilinePatch = errors.New("patch line contains newline characters")
	// ErrNotRegularFile is returned by PatchFile when the target path is not a regular file.
	ErrNotRegularFile = errors.New("not a regular file")
)

// LineMatch matches a single text line. It is created by ByPrefix, ByExact,
// ByContains, or ByRegex and consumed by Patch constructors.
type LineMatch struct {
	fn  func(string) bool
	err error
}

// ByPrefix returns a LineMatch that matches lines beginning with s.
func ByPrefix(s string) LineMatch {
	return LineMatch{fn: func(line string) bool { return strings.HasPrefix(line, s) }}
}

// ByExact returns a LineMatch that matches lines equal to s.
func ByExact(s string) LineMatch {
	return LineMatch{fn: func(line string) bool { return line == s }}
}

// ByContains returns a LineMatch that matches lines containing s.
func ByContains(s string) LineMatch {
	return LineMatch{fn: func(line string) bool { return strings.Contains(line, s) }}
}

// ByRegex returns a LineMatch that matches lines matching pattern.
// If pattern fails to compile, the error surfaces when the Patch is applied in PatchFile.
func ByRegex(pattern string) LineMatch {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return LineMatch{err: fmt.Errorf("invalid regex %q: %w", pattern, err)}
	}
	return LineMatch{fn: re.MatchString}
}

// checkMatch validates that a LineMatch was created by one of the constructors.
// A zero-value LineMatch (both fn and err nil) is rejected with ErrInvalidMatch.
func checkMatch(match LineMatch) error {
	if match.err != nil {
		return match.err
	}
	if match.fn == nil {
		return ErrInvalidMatch
	}
	return nil
}

// checkLine rejects line values containing newline characters, which would
// corrupt the line-oriented structure of the patched file.
func checkLine(line string) error {
	if strings.ContainsAny(line, "\r\n") {
		return fmt.Errorf("%w: %q", ErrMultilinePatch, line)
	}
	return nil
}

// Patch is a single file-editing operation that transforms a slice of lines.
// Construct patches with ReplaceOrAppend, DeleteMatching, AppendIfMissing,
// InsertAfter, InsertBefore, or Transform.
type Patch struct {
	apply func(lines []string) ([]string, error)
	err   error
}

// ReplaceOrAppend replaces the first line matching match with line,
// or appends line if no existing line matches.
func ReplaceOrAppend(match LineMatch, line string) Patch {
	if err := checkMatch(match); err != nil {
		return Patch{err: err}
	}
	if err := checkLine(line); err != nil {
		return Patch{err: err}
	}
	return Patch{apply: func(lines []string) ([]string, error) {
		for i, l := range lines {
			if match.fn(l) {
				lines[i] = line
				return lines, nil
			}
		}
		return append(lines, line), nil
	}}
}

// DeleteMatching removes all lines matching match.
func DeleteMatching(match LineMatch) Patch {
	if err := checkMatch(match); err != nil {
		return Patch{err: err}
	}
	return Patch{apply: func(lines []string) ([]string, error) {
		result := lines[:0]
		for _, l := range lines {
			if !match.fn(l) {
				result = append(result, l)
			}
		}
		return result, nil
	}}
}

// AppendIfMissing appends line if no existing line is exactly equal to it.
func AppendIfMissing(line string) Patch {
	if err := checkLine(line); err != nil {
		return Patch{err: err}
	}
	return Patch{apply: func(lines []string) ([]string, error) {
		if slices.Contains(lines, line) {
			return lines, nil
		}
		return append(lines, line), nil
	}}
}

// InsertAfter inserts line after the first line matching match.
// If no line matches, the file is left unchanged.
func InsertAfter(match LineMatch, line string) Patch {
	if err := checkMatch(match); err != nil {
		return Patch{err: err}
	}
	if err := checkLine(line); err != nil {
		return Patch{err: err}
	}
	return Patch{apply: func(lines []string) ([]string, error) {
		for i, l := range lines {
			if match.fn(l) {
				result := make([]string, 0, len(lines)+1)
				result = append(result, lines[:i+1]...)
				result = append(result, line)
				result = append(result, lines[i+1:]...)
				return result, nil
			}
		}
		return lines, nil
	}}
}

// InsertBefore inserts line before the first line matching match.
// If no line matches, the file is left unchanged.
func InsertBefore(match LineMatch, line string) Patch {
	if err := checkMatch(match); err != nil {
		return Patch{err: err}
	}
	if err := checkLine(line); err != nil {
		return Patch{err: err}
	}
	return Patch{apply: func(lines []string) ([]string, error) {
		for i, l := range lines {
			if match.fn(l) {
				result := make([]string, 0, len(lines)+1)
				result = append(result, lines[:i]...)
				result = append(result, line)
				result = append(result, lines[i:]...)
				return result, nil
			}
		}
		return lines, nil
	}}
}

// Transform applies fn to the full slice of lines and is an escape hatch for
// cases that do not fit the structured patch constructors.
func Transform(fn func(lines []string) ([]string, error)) Patch {
	return Patch{apply: fn}
}

// applyPatches runs each patch in sequence and returns the resulting lines.
func applyPatches(patches []Patch, lines []string) ([]string, error) {
	for _, p := range patches {
		if p.err != nil {
			return nil, p.err
		}
		if p.apply == nil {
			return nil, ErrNilPatch
		}
		var err error
		lines, err = p.apply(lines)
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

// buildOutput reconstructs the file bytes from lines, restoring the original
// trailing-newline status and CRLF encoding.
func buildOutput(lines []string, hadTrailingNewline bool, crlf bool) []byte {
	out := strings.Join(lines, "\n")
	if hadTrailingNewline && len(lines) > 0 {
		out += "\n"
	}
	outBytes := []byte(out)
	if crlf {
		outBytes = bytes.ReplaceAll(outBytes, []byte("\n"), []byte("\r\n"))
	}
	return outBytes
}

// PatchOption is a functional option for PatchFile.
type PatchOption func(*patchOptions)

type patchOptions struct {
	create bool
	perm   fs.FileMode
}

// WithCreate causes PatchFile to create the file with the given permissions if
// it does not already exist. Without this option, PatchFile returns an error when
// the target file is missing.
func WithCreate(perm fs.FileMode) PatchOption {
	return func(o *patchOptions) {
		o.create = true
		o.perm = perm.Perm() // strip any type or special bits
	}
}

// statAndRead returns the file's permission bits and content. When the file does
// not exist and opts.create is set, it returns the create permission and nil
// content. In all other error cases it returns a wrapped error.
//
// Only the lower 9 permission bits (rwxrwxrwx) are returned; setuid, setgid,
// and sticky bits are not preserved by PatchFile.
func statAndRead(fsys FS, path string, opts *patchOptions) (fs.FileMode, []byte, error) {
	info, statErr := fsys.Stat(path)
	switch {
	case statErr == nil:
		// Allow regular files and symlinks; reject directories, FIFOs, devices, etc.
		// Symlinks are allowed because stat(2) may report ModeSymlink on BSD
		// (which does not follow links by default), while the subsequent ReadFile
		// and WriteFileAtomic still operate on the link target or replace the link.
		if typ := info.Mode().Type(); typ != 0 && typ != fs.ModeSymlink {
			return 0, nil, fmt.Errorf("patch-file %s: %w", path, ErrNotRegularFile)
		}
		content, err := fsys.ReadFile(path)
		if err != nil {
			return 0, nil, fmt.Errorf("patch-file %s: %w", path, err)
		}
		perm := info.Mode().Perm()
		if info.Mode().Type() == fs.ModeSymlink {
			// BSD stat reports symlink permissions as 0o777, which are
			// meaningless. Use a safe private default; on GNU/Linux stat
			// follows the link so this branch is never reached.
			perm = 0o600
		}
		return perm, content, nil
	case errors.Is(statErr, fs.ErrNotExist):
		if !opts.create {
			return 0, nil, fmt.Errorf("patch-file %s: %w", path, statErr)
		}
		return opts.perm, nil, nil
	default:
		return 0, nil, fmt.Errorf("patch-file %s: %w", path, statErr)
	}
}

// PatchFile reads path, applies each patch in sequence, and writes the result
// back atomically via WriteFileAtomic. Existing permission bits (rwxrwxrwx) are
// preserved; setuid, setgid, and sticky bits are not. If the file does not exist
// and WithCreate is provided, the file is created with the given mode; otherwise
// ErrNotExist is returned. Non-regular files (directories, FIFOs, devices) are
// rejected with ErrNotRegularFile.
//
// If path is a symlink, the link itself is replaced by the rewritten file; the
// symlink target is not modified. On platforms where stat follows symlinks (GNU
// Linux), the target's permission bits are preserved. On platforms where stat
// reports the link itself (BSD), symlink permissions are meaningless (0o777), so
// the replacement file is created with 0o600 instead.
//
// CRLF handling: if the original file contains any CR+LF sequence, the entire
// output is written with CR+LF line endings. Files with mixed line endings are
// fully normalised to CRLF. Bare CR bytes (old Mac-style) are not treated as
// line endings and are left as-is. Otherwise LF is used throughout.
//
// The trailing newline status of the original file is preserved: files that end
// with a newline will continue to do so; files that do not will not gain one.
// New files created via WithCreate get a trailing newline when the output is
// non-empty; an empty result (or an existing empty file) produces an empty file.
//
// Because WriteFileAtomic replaces the original inode via a temp-file rename,
// only permission bits are restored. Owner, group, ACLs, extended attributes,
// hard links, and timestamps of the original file are not preserved.
//
// PatchFile performs a read-then-write cycle without inter-process locking. A
// concurrent writer between the read and the rename will have its changes
// silently overwritten by the atomic rename.
//
// If the patches produce output identical to the original content, WriteFileAtomic
// is skipped and the file (including any symlink) is left untouched.
func PatchFile(fsys FS, path string, patches []Patch, opts ...PatchOption) error {
	options := &patchOptions{}
	for _, opt := range opts {
		opt(options)
	}

	perm, content, err := statAndRead(fsys, path, options)
	if err != nil {
		return err
	}

	// Detect and normalise CRLF so all patch logic operates on LF-only lines.
	crlf := bytes.Contains(content, []byte("\r\n"))
	text := string(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")))

	// Preserve the trailing newline status of the original. A nil content slice
	// means the file does not yet exist (WithCreate path) and is treated as
	// having a trailing newline so the first write is properly terminated.
	// An existing empty file (non-nil, zero-length) is not treated as having a
	// trailing newline; its status is determined by the content, which has none.
	hadTrailingNewline := content == nil || strings.HasSuffix(text, "\n")

	// Split on newlines; strip one trailing newline to avoid a ghost empty line.
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}

	lines, err = applyPatches(patches, lines)
	if err != nil {
		return fmt.Errorf("patch-file %s: %w", path, err)
	}

	outBytes := buildOutput(lines, hadTrailingNewline, crlf)

	// Skip the write when the patches produced no change. The nil check is
	// required because bytes.Equal(nil, []byte{}) is true: without it, a new
	// file (content==nil) whose patches yield empty output would never be created.
	if content != nil && bytes.Equal(outBytes, content) {
		return nil
	}

	if err := WriteFileAtomic(fsys, path, outBytes, perm); err != nil {
		return fmt.Errorf("patch-file %s: %w", path, err)
	}
	return nil
}
