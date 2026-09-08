// Command apidiff reports exported API differences between two checkouts of
// the rig module and writes them as Markdown.
//
// It compares once per GOOS given, which covers the platform constraints this
// module uses. API behind a GOARCH constraint, or behind a GOOS that is not in
// the list, is not seen.
//
// Exit status reports what was found, so a caller can act on it without
// parsing the output:
//
//	0  the exported API is unchanged
//	1  one of the checkouts could not be inspected
//	3  the API changed, but every change is backwards compatible
//	4  an incompatible change is present
//
// Status 2 is deliberately never returned. The flag package exits with 2 when
// it rejects an argument, and a panicking Go program exits with 2 as well, so
// a caller that treated 2 as a result would report a crash as an API change.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/exp/apidiff"
	"golang.org/x/tools/go/packages"
)

const (
	exitUnchanged    = 0
	exitError        = 1
	exitCompatible   = 3
	exitIncompatible = 4
)

var (
	errLoad = errors.New("failed to inspect packages")
	errGOOS = errors.New("invalid GOOS list")

	goosPattern = regexp.MustCompile(`^[a-z0-9]{2,12}$`)
)

func main() {
	baseDir := flag.String("base", "", "directory holding the checkout to compare against (required)")
	headDir := flag.String("head", ".", "directory holding the checkout to compare")
	goosList := flag.String("goos", "linux,windows,darwin",
		"comma separated GOOS values to compare")
	flag.Parse()

	if *baseDir == "" {
		fmt.Fprintln(os.Stderr, "apidiff: -base is required")
		flag.Usage()
		os.Exit(exitError)
	}

	platforms, err := parseGOOS(*goosList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "apidiff: %v\n", err)
		os.Exit(exitError)
	}

	status, err := run(os.Stdout, *baseDir, *headDir, platforms)
	if err != nil {
		fmt.Fprintf(os.Stderr, "apidiff: %v\n", err)
		os.Exit(exitError)
	}
	os.Exit(status)
}

func parseGOOS(list string) ([]string, error) {
	var platforms []string
	for goos := range strings.SplitSeq(list, ",") {
		goos = strings.TrimSpace(goos)
		if goos == "" {
			continue
		}
		if !goosPattern.MatchString(goos) {
			return nil, fmt.Errorf("%w: %q", errGOOS, goos)
		}
		platforms = append(platforms, goos)
	}
	if len(platforms) == 0 {
		return nil, fmt.Errorf("%w: no values", errGOOS)
	}

	return platforms, nil
}

func run(out io.Writer, baseDir, headDir string, platforms []string) (int, error) {
	var changes []apidiff.Change
	for _, goos := range platforms {
		base, err := load(baseDir, goos)
		if err != nil {
			return exitError, fmt.Errorf("base checkout %s (GOOS=%s): %w", baseDir, goos, err)
		}
		head, err := load(headDir, goos)
		if err != nil {
			return exitError, fmt.Errorf("head checkout %s (GOOS=%s): %w", headDir, goos, err)
		}

		// A symbol diff across module paths would list the whole API twice,
		// once removed and once added.
		if base.Path != head.Path {
			writeReport(out, []string{fmt.Sprintf(
				"module path changed from %s to %s: every import path moves, so nothing built against %s keeps compiling",
				base.Path, head.Path, base.Path)}, nil, platforms)

			return exitIncompatible, nil
		}

		changes = append(changes, apidiff.ModuleChanges(base, head).Changes...)
	}

	broken, added := partition(changes)

	writeReport(out, broken, added, platforms)

	switch {
	case len(broken) > 0:
		return exitIncompatible, nil
	case len(added) > 0:
		return exitCompatible, nil
	default:
		return exitUnchanged, nil
	}
}

// partition splits changes into incompatible and compatible messages, sorted
// and deduplicated: a change outside a build constraint is reported once per
// GOOS, and apidiff reports a generic type's method once per type parameter.
func partition(changes []apidiff.Change) (broken, added []string) {
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		if _, dup := seen[change.Message]; dup {
			continue
		}
		seen[change.Message] = struct{}{}
		if change.Compatible {
			added = append(added, change.Message)

			continue
		}
		broken = append(broken, change.Message)
	}
	sort.Strings(broken)
	sort.Strings(added)

	return broken, added
}

func writeReport(out io.Writer, broken, added, platforms []string) {
	if len(broken) == 0 && len(added) == 0 {
		fmt.Fprintf(out, "No exported API changes (%s).\n", strings.Join(platforms, ", "))

		return
	}

	fmt.Fprintf(out, "Compared for %s. Entries read ./package.Symbol; the module root package is unprefixed.\n\n",
		strings.Join(platforms, ", "))

	if len(broken) > 0 {
		fmt.Fprintf(out, "Incompatible changes (%d) - these break code that compiles against the base version:\n\n", len(broken))
		writeList(out, broken)
	}

	if len(added) > 0 {
		fmt.Fprintf(out, "Compatible changes (%d):\n\n", len(added))
		writeList(out, added)
	}
}

func writeList(out io.Writer, messages []string) {
	for _, message := range messages {
		// apidiff emits multi-line messages for type changes; indent the
		// continuation lines to keep one entry per item.
		fmt.Fprintf(out, "- %s\n", strings.ReplaceAll(strings.TrimSpace(message), "\n", "\n  "))
	}
	fmt.Fprintln(out)
}

func load(dir, goos string) (*apidiff.Module, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps |
			packages.NeedModule | packages.NeedFiles,
		Dir: dir,
		// A cross-GOOS load cannot use cgo. This drops files importing "C"
		// along with whatever they export, which is checked for below.
		Env: append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0"),
	}

	loaded, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}

	var (
		path     string
		exported []*types.Package
		failed   []string
	)
	for _, pkg := range loaded {
		for _, pkgErr := range pkg.Errors {
			failed = append(failed, fmt.Sprintf("%s: %v", pkg.PkgPath, pkgErr))
		}
		// IgnoredFiles legitimately includes the other platforms' files. A
		// file importing "C" is excluded on every platform though, so nothing
		// it exports would ever be compared.
		for _, ignored := range pkg.IgnoredFiles {
			if importsC(ignored) {
				failed = append(failed,
					fmt.Sprintf("%s: %s uses cgo, which this comparison cannot inspect", pkg.PkgPath, ignored))
			}
		}
		if pkg.Module != nil && path == "" {
			path = pkg.Module.Path
		}
		if pkg.Types == nil || isInternal(pkg.PkgPath) {
			continue
		}
		exported = append(exported, pkg.Types)
	}

	// A package that fails to load is indistinguishable from a deleted one,
	// which would be reported as a fabricated incompatible change.
	if len(failed) > 0 {
		return nil, fmt.Errorf("%w:\n%s", errLoad, strings.Join(failed, "\n"))
	}
	if len(exported) == 0 {
		return nil, fmt.Errorf("%w: no packages found", errLoad)
	}

	return &apidiff.Module{Path: path, Packages: exported}, nil
}

// importsC reports whether the file at path imports "C". An unparseable file
// is not cgo: it is either not Go, or broken in a way the type checker reports.
func importsC(path string) bool {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return false
	}
	for _, imported := range parsed.Imports {
		if imported.Path != nil && imported.Path.Value == `"C"` {
			return true
		}
	}

	return false
}

func isInternal(pkgPath string) bool {
	return pkgPath == "internal" ||
		strings.HasSuffix(pkgPath, "/internal") ||
		strings.Contains(pkgPath, "/internal/")
}
