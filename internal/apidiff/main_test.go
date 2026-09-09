package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/exp/apidiff"
)

var allPlatforms = []string{"linux", "windows", "darwin"}

// writeModule creates a throwaway module whose exported API is whatever the
// caller puts in files, keyed by name within a single package directory.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.test/apitest\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	pkgDir := filepath.Join(dir, "thing")
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	return dir
}

const keptAPI = `package thing

// Kept is present in every version used by the tests.
func Kept() {}
`

func module(body string) map[string]string {
	return map[string]string{"thing.go": body}
}

func TestRunStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     map[string]string
		head     map[string]string
		want     int
		contains string
	}{
		{
			name:     "unchanged",
			base:     module(keptAPI),
			head:     module(keptAPI),
			want:     exitUnchanged,
			contains: "No exported API changes",
		},
		{
			name:     "compatible addition",
			base:     module(keptAPI),
			head:     module(keptAPI + "\n// Added is new.\nfunc Added() {}\n"),
			want:     exitCompatible,
			contains: "Added: added",
		},
		{
			name:     "incompatible removal",
			base:     module(keptAPI + "\n// Doomed goes away.\nfunc Doomed() {}\n"),
			head:     module(keptAPI),
			want:     exitIncompatible,
			contains: "Doomed: removed",
		},
		{
			name:     "incompatible signature change",
			base:     module(keptAPI),
			head:     module(strings.Replace(keptAPI, "func Kept() {}", "func Kept(n int) {}", 1)),
			want:     exitIncompatible,
			contains: "Kept: changed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			status, err := run(&out, writeModule(t, test.base), writeModule(t, test.head), allPlatforms)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if status != test.want {
				t.Errorf("status = %d, want %d\n%s", status, test.want, out.String())
			}
			if !strings.Contains(out.String(), test.contains) {
				t.Errorf("report does not mention %q:\n%s", test.contains, out.String())
			}
		})
	}
}

// Part of the exported API sits behind build constraints, so a comparison that
// only type-checks the host platform silently misses changes to it.
func TestRunSeesConstrainedAPI(t *testing.T) {
	t.Parallel()

	const windowsOnly = `//go:build windows

package thing

// OnlyOnWindows is exported on Windows alone.
func OnlyOnWindows() {}
`

	base := writeModule(t, map[string]string{"thing.go": keptAPI, "thing_windows.go": windowsOnly})
	head := writeModule(t, module(keptAPI))

	t.Run("windows included", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder
		status, err := run(&out, base, head, allPlatforms)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if status != exitIncompatible {
			t.Errorf("status = %d, want %d\n%s", status, exitIncompatible, out.String())
		}
		if !strings.Contains(out.String(), "OnlyOnWindows: removed") {
			t.Errorf("removal behind a build constraint was missed:\n%s", out.String())
		}
	})

	t.Run("linux alone cannot see it", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder
		status, err := run(&out, base, head, []string{"linux"})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if status != exitUnchanged {
			t.Errorf("status = %d, want %d: this documents why every GOOS is compared\n%s",
				status, exitUnchanged, out.String())
		}
	})
}

// Bumping the module path is the one change that moves every import path, and
// it must not be reported as an unrelated pile of removals and additions.
func TestRunModulePathChange(t *testing.T) {
	t.Parallel()

	base := writeModule(t, module(keptAPI))
	head := writeModule(t, module(keptAPI))
	if err := os.WriteFile(filepath.Join(head, "go.mod"),
		[]byte("module example.test/apitest/v3\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("rewrite go.mod: %v", err)
	}

	var out strings.Builder
	status, err := run(&out, base, head, allPlatforms)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if status != exitIncompatible {
		t.Errorf("status = %d, want %d\n%s", status, exitIncompatible, out.String())
	}
	if !strings.Contains(out.String(), "module path changed from example.test/apitest to example.test/apitest/v3") {
		t.Errorf("report does not name the module path change:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Kept") {
		t.Errorf("report lists individual symbols instead of the path change:\n%s", out.String())
	}
}

// A package that does not compile is indistinguishable from a deleted one, so
// a partial load has to be refused rather than reported as removals.
func TestRunRefusesPartialLoad(t *testing.T) {
	t.Parallel()

	broken := module("package thing\n\nfunc Kept() { this is not go }\n")

	for _, test := range []struct {
		name string
		base map[string]string
		head map[string]string
	}{
		{name: "base does not compile", base: broken, head: module(keptAPI)},
		{name: "head does not compile", base: module(keptAPI), head: broken},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			status, err := run(&out, writeModule(t, test.base), writeModule(t, test.head), allPlatforms)
			if !errors.Is(err, errLoad) {
				t.Fatalf("err = %v, want errLoad", err)
			}
			if status != exitError {
				t.Errorf("status = %d, want %d", status, exitError)
			}
			if out.Len() != 0 {
				t.Errorf("wrote a report despite failing to load:\n%s", out.String())
			}
		})
	}
}

// The comparison runs with cgo off, which drops any file importing "C" along
// with whatever it exports. That must be refused rather than reported as an
// unchanged API.
func TestRunRefusesCgo(t *testing.T) {
	t.Parallel()

	const cgoFile = `package thing

import "C"

// ExportedFromCgo would never be compared.
func ExportedFromCgo() {}
`

	var out strings.Builder
	status, err := run(&out,
		writeModule(t, map[string]string{"thing.go": keptAPI, "thing_cgo.go": cgoFile}),
		writeModule(t, module(keptAPI)),
		allPlatforms)

	if !errors.Is(err, errLoad) {
		t.Fatalf("err = %v, want errLoad", err)
	}
	if !strings.Contains(err.Error(), "uses cgo") {
		t.Errorf("error does not explain the cause: %v", err)
	}
	if status != exitError {
		t.Errorf("status = %d, want %d", status, exitError)
	}
}

func TestImportsC(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, body := range map[string]string{
		"cgo.go":      "package thing\n\nimport \"C\"\n",
		"grouped.go":  "package thing\n\nimport (\n\t\"fmt\"\n\t\"C\"\n)\n",
		"plain.go":    "package thing\n\nimport \"fmt\"\n",
		"notc.go":     "package thing\n\nimport \"github.com/example/C\"\n",
		"unparseable": "this is not go at all",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for name, want := range map[string]bool{
		"cgo.go":      true,
		"grouped.go":  true,
		"plain.go":    false,
		"notc.go":     false,
		"unparseable": false,
		"absent.go":   false,
	} {
		if got := importsC(filepath.Join(dir, name)); got != want {
			t.Errorf("importsC(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestRunMissingDirectory(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	status, err := run(&out, filepath.Join(t.TempDir(), "absent"), writeModule(t, module(keptAPI)), allPlatforms)
	if err == nil {
		t.Fatal("expected an error for a directory that does not exist")
	}
	if status != exitError {
		t.Errorf("status = %d, want %d", status, exitError)
	}
}

func TestParseGOOS(t *testing.T) {
	t.Parallel()

	t.Run("accepted", func(t *testing.T) {
		t.Parallel()

		got, err := parseGOOS(" linux , windows ,,darwin")
		if err != nil {
			t.Fatalf("parseGOOS: %v", err)
		}
		if !equal(got, allPlatforms) {
			t.Errorf("got %q, want %q", got, allPlatforms)
		}
	})

	for _, list := range []string{"", " ", ",", "linux;windows", "GOOS", "a", "../etc", "linux$(id)"} {
		t.Run("rejected "+list, func(t *testing.T) {
			t.Parallel()

			if _, err := parseGOOS(list); !errors.Is(err, errGOOS) {
				t.Errorf("parseGOOS(%q) err = %v, want errGOOS", list, err)
			}
		})
	}
}

func TestPartition(t *testing.T) {
	t.Parallel()

	broken, added := partition([]apidiff.Change{
		{Message: "Zeta: removed", Compatible: false},
		{Message: "Beta: added", Compatible: true},
		// Unconstrained changes repeat once per GOOS, and apidiff repeats a
		// generic type's method once per type parameter.
		{Message: "Beta: added", Compatible: true},
		{Message: "Generic[T].Method: added", Compatible: true},
		{Message: "Generic[T].Method: added", Compatible: true},
		{Message: "Alpha: removed", Compatible: false},
		{Message: "Alpha: removed", Compatible: false},
	})

	wantBroken := []string{"Alpha: removed", "Zeta: removed"}
	wantAdded := []string{"Beta: added", "Generic[T].Method: added"}

	if !equal(broken, wantBroken) {
		t.Errorf("broken = %q, want %q", broken, wantBroken)
	}
	if !equal(added, wantAdded) {
		t.Errorf("added = %q, want %q", added, wantAdded)
	}
}

func TestWriteReport(t *testing.T) {
	t.Parallel()

	t.Run("no changes names the platforms", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder
		writeReport(&out, nil, nil, allPlatforms)

		if got := out.String(); got != "No exported API changes (linux, windows, darwin).\n" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("both sections", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder
		writeReport(&out, []string{"Gone: removed"}, []string{"New: added"}, allPlatforms)

		for _, want := range []string{
			"Compared for linux, windows, darwin.",
			"Incompatible changes (1)",
			"- Gone: removed",
			"Compatible changes (1)",
			"- New: added",
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("missing %q in:\n%s", want, out.String())
			}
		}
	})

	t.Run("multi-line message stays one item", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder
		writeReport(&out, []string{"Thing: changed\nfrom func()\nto func(int)"}, nil, allPlatforms)

		if !strings.Contains(out.String(), "- Thing: changed\n  from func()\n  to func(int)\n") {
			t.Errorf("continuation lines are not indented:\n%s", out.String())
		}
	})
}

func TestIsInternal(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"example.test/apitest/thing":              false,
		"example.test/apitest/internal":           true,
		"example.test/apitest/internal/thing":     true,
		"internal":                                true,
		"example.test/apitest/internalish":        false,
		"example.test/apitest/thing/internal/sub": true,
	} {
		if got := isInternal(path); got != want {
			t.Errorf("isInternal(%q) = %v, want %v", path, got, want)
		}
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
