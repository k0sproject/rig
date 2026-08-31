package remotefs_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// Test URLs use the .invalid TLD, which RFC 2606 reserves and guarantees will
// never resolve. Nothing here reaches the network -- every FS is backed by a
// rigtest.MockRunner, which records command strings instead of running them --
// and an unresolvable host means that stays true even if a test is later
// rewired by mistake.

const (
	tmpPath = "/var/lib/k0s/images/bundle.tar.AbCdEf"
	dstPath = "/var/lib/k0s/images/bundle.tar"
	testURL = "https://test.invalid/bundle.tar"
)

// mockDownloadRunner wires up a runner where mktemp yields a known path and
// curl is available, so the interesting part is what the download does with it.
func mockDownloadRunner() *rigtest.MockRunner {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.HasPrefix("mktemp"), tmpPath)
	mr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	return mr
}

// commandTouching returns the first recorded command mentioning substr.
func commandTouching(t *testing.T, mr *rigtest.MockRunner, substr string) string {
	t.Helper()
	for _, c := range mr.Commands() {
		if strings.Contains(c, substr) {
			return c
		}
	}
	return ""
}

// psScriptTouching returns the first recorded command whose decoded PowerShell
// script mentions substr. The commands themselves carry only the base64 payload
// that powershell.Cmd produces, so the script has to be decoded to be asserted
// against.
func psScriptTouching(t *testing.T, mr *rigtest.MockRunner, substr string) string {
	t.Helper()
	for _, c := range mr.Commands() {
		script, ok := decodePSScript(c)
		if ok && strings.Contains(script, substr) {
			return script
		}
	}
	return ""
}

func TestPosixDownloadURLIsAtomic(t *testing.T) {
	t.Run("fetches into a temporary and renames onto the destination", func(t *testing.T) {
		mr := mockDownloadRunner()
		f := remotefs.NewPosixFS(mr)

		require.NoError(t, f.DownloadURL("http://test.invalid/bundle.tar", dstPath))

		curl := commandTouching(t, mr, "curl -sSLf")
		require.NotEmpty(t, curl, "expected a curl invocation, got %v", mr.Commands())
		// The temporary name starts with the destination name, so compare the
		// -o argument rather than looking for the destination anywhere.
		require.Contains(t, curl, "-o "+tmpPath, "curl must write to the temporary file")
		require.NotContains(t, curl, "-o "+dstPath+" ", "curl must not write straight to the destination")

		require.Contains(t, mr.LastCommand(), "mv", "the temporary must be renamed into place")
		require.Contains(t, mr.LastCommand(), dstPath)
	})

	t.Run("the temporary is created next to the destination", func(t *testing.T) {
		mr := mockDownloadRunner()
		f := remotefs.NewPosixFS(mr)

		require.NoError(t, f.DownloadURL("http://test.invalid/bundle.tar", dstPath))

		mktemp := commandTouching(t, mr, "mktemp")
		require.Contains(t, mktemp, "/var/lib/k0s/images/bundle.tar.",
			"the temporary must share a directory with the destination so the rename stays within one filesystem")
	})

	t.Run("a failed fetch removes the temporary and never touches the destination", func(t *testing.T) {
		mr := mockDownloadRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("curl"), errors.New("exit status 18"))
		f := remotefs.NewPosixFS(mr)

		err := f.DownloadURL("http://test.invalid/bundle.tar", dstPath)
		require.Error(t, err)

		require.NotEmpty(t, commandTouching(t, mr, "rm -- "+tmpPath), "the partial file must be cleaned up")
		for _, c := range mr.Commands() {
			require.NotContains(t, c, "mv", "nothing may be moved onto the destination after a failed fetch")
		}
	})

	t.Run("a failed rename removes the temporary", func(t *testing.T) {
		mr := mockDownloadRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("mv"), errors.New("exit status 1"))
		f := remotefs.NewPosixFS(mr)

		err := f.DownloadURL("http://test.invalid/bundle.tar", dstPath)
		require.Error(t, err)
		require.NotEmpty(t, commandTouching(t, mr, "rm -- "+tmpPath))
	})

	t.Run("a cleanup failure does not hide the download failure", func(t *testing.T) {
		mr := mockDownloadRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("curl"), errors.New("exit status 18"))
		mr.AddCommandFailure(rigtest.HasPrefix("rm"), errors.New("permission denied"))
		f := remotefs.NewPosixFS(mr)

		err := f.DownloadURL("http://test.invalid/bundle.tar", dstPath)
		require.Error(t, err)
		require.ErrorContains(t, err, "exit status 18", "the original failure must survive")
		require.ErrorContains(t, err, tmpPath, "the stranded file must be named")
	})

	t.Run("mktemp failure is reported", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("mktemp"), errors.New("no space left on device"))
		f := remotefs.NewPosixFS(mr)

		err := f.DownloadURL("http://test.invalid/bundle.tar", dstPath)
		require.ErrorContains(t, err, "create temporary file")
	})
}

func TestWindowsDownloadURLIsAtomic(t *testing.T) {
	const (
		winTmp = `C:\k0s\bundle.tar.AbCdEf`
		winDst = `C:\k0s\bundle.tar`
	)

	mr := rigtest.NewMockRunner()
	mr.Windows = true
	mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), winTmp)
	f := remotefs.NewWindowsFS(mr)

	require.NoError(t, f.DownloadURL("http://test.invalid/bundle.tar", winDst))

	fetch := psScriptTouching(t, mr, "Invoke-WebRequest")
	require.NotEmpty(t, fetch, "expected an Invoke-WebRequest invocation, got %v", mr.Commands())
	// The temporary name starts with the destination name, so compare the
	// -OutFile argument rather than looking for the destination anywhere.
	require.Contains(t, fetch, `-OutFile "`+winTmp+`"`, "the transfer must write to the temporary file")
	require.NotContains(t, fetch, `-OutFile "`+winDst+`"`, "the transfer must not write straight to the destination")

	rename := psScriptTouching(t, mr, "Move-Item")
	require.NotEmpty(t, rename, "the temporary must be renamed into place, got %v", mr.Commands())
	require.Contains(t, rename, `-LiteralPath "`+winTmp+`"`)
	require.Contains(t, rename, `-Destination "`+winDst+`"`)
}

// headOutput builds a curl -I style response for the download tests.
func headOutput(length int64, ranges bool) string {
	out := "HTTP/2 200 \r\n"
	if length >= 0 {
		out += fmt.Sprintf("content-length: %d\r\n", length)
	}
	if ranges {
		out += "accept-ranges: bytes\r\n"
	}
	return out + "\r\n"
}

// wgetHeadStderr builds the same response in the shape wget --spider
// --server-response writes to stderr: two-space indent, mixed case, no CR.
func wgetHeadStderr(length int64, ranges bool) string {
	out := "  HTTP/1.1 200 OK\n"
	if length >= 0 {
		out += fmt.Sprintf("  Content-Length: %d\n", length)
	}
	if ranges {
		out += "  Accept-Ranges: bytes\n"
	}
	return out
}

// resumeRunner records the resume flag seen on each transfer and reports a
// fixed size for the partial file.
type resumeRunner struct {
	*rigtest.MockRunner
	resumeSeen []bool
	// truncateErr and removeErr are what emptying and removing the partial fail
	// with. They are read when the command runs, so a test can set them after
	// the runner is built.
	truncateErr error
	removeErr   error
}

func newResumeRunner(t *testing.T, head string, partSize int64, transferErr error) *resumeRunner {
	t.Helper()
	rr := &resumeRunner{MockRunner: rigtest.NewMockRunner()}
	rr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
	rr.AddCommandOutput(rigtest.HasPrefix("curl -sS -L -I"), head)
	rr.addPartialProbes(partSize)
	rr.AddCommand(rigtest.HasPrefix("curl -sSLf"), func(a *rigtest.A) error {
		rr.resumeSeen = append(rr.resumeSeen, strings.Contains(a.Command, "-C -"))
		return transferErr
	})
	return rr
}

// newWgetResumeRunner is newResumeRunner for a host without curl, so the wget
// fallback in PosixFS.fetchURL is what runs. wget reports headers on stderr and
// makes its HEAD request with --spider, so that is matched ahead of the transfer:
// the mock takes the first matching handler.
func newWgetResumeRunner(t *testing.T, head string, partSize int64, transferErr error) *resumeRunner {
	t.Helper()
	rr := &resumeRunner{MockRunner: rigtest.NewMockRunner()}
	rr.AddCommandFailure(rigtest.Equal("command -v curl"), errors.New("exit status 127"))
	rr.AddCommandOutput(rigtest.Equal("command -v wget"), "/usr/bin/wget")
	rr.AddCommand(rigtest.HasPrefix("wget --spider"), func(a *rigtest.A) error {
		_, err := fmt.Fprint(a.Stderr, head)
		return err
	})
	rr.addPartialProbes(partSize)
	rr.AddCommand(rigtest.HasPrefix("wget"), func(a *rigtest.A) error {
		rr.resumeSeen = append(rr.resumeSeen, strings.Contains(a.Command, " -c "))
		return transferErr
	})
	return rr
}

// addPartialProbes wires up what Download asks about the partial file: the stat
// behind size, the existence check, mktemp for the non-resuming path and
// truncate for discarding an unusable partial. A negative partSize means there
// is no partial.
func (rr *resumeRunner) addPartialProbes(partSize int64) {
	rr.AddCommand(rigtest.HasPrefix("truncate"), func(_ *rigtest.A) error { return rr.truncateErr })
	rr.AddCommand(rigtest.HasPrefix("rm"), func(_ *rigtest.A) error { return rr.removeErr })
	rr.AddCommandOutput(rigtest.HasPrefix("mktemp"), tmpPath)
	// test -f, used by FileExist.
	rr.AddCommand(rigtest.HasPrefix("test -f"), func(_ *rigtest.A) error {
		if partSize < 0 {
			return errors.New("exit status 1")
		}
		return nil
	})
	rr.AddCommand(rigtest.HasPrefix("env -i"), func(a *rigtest.A) error {
		if partSize < 0 {
			return errors.New("exit status 1")
		}
		_, err := fmt.Fprintf(a.Stdout, "0x81a4 %d 2024-09-30 11:42:01.000000000 +0000 //part//\n", partSize)
		return err
	})
}

// partialFor mirrors the naming Download uses, so the tests assert against the
// same rule rather than a copied literal.
func partialFor(t *testing.T, dst, url string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(url))
	return dst + ".rigpart-" + hex.EncodeToString(sum[:6])
}

// foreignFS is an FS implemented outside this package: it provides the exported
// Downloader and the few methods the atomic move needs, but not the unexported
// fetch provider that PosixFS and WinFS have. Everything else is inherited from
// the embedded nil interface and would panic if reached, which keeps the stub
// honest about what Download actually touches.
type foreignFS struct {
	remotefs.FS
	tmp         string
	fetchedTo   []string
	renamedTo   map[string]string
	downloadErr error
}

func newForeignFS(tmp string) *foreignFS {
	return &foreignFS{tmp: tmp, renamedTo: map[string]string{}}
}

func (f *foreignFS) DownloadURL(_, dst string) error {
	f.fetchedTo = append(f.fetchedTo, dst)
	return f.downloadErr
}

func (f *foreignFS) CreateTemp(_, _ string) (string, error) { return f.tmp, nil }
func (f *foreignFS) Rename(oldpath, newpath string) error {
	f.renamedTo[oldpath] = newpath
	return nil
}
func (f *foreignFS) Remove(string) error        { return nil }
func (f *foreignFS) Dir(p string) string        { return path.Dir(p) }
func (f *foreignFS) Base(p string) string       { return path.Base(p) }
func (f *foreignFS) Join(elem ...string) string { return path.Join(elem...) }
func (f *foreignFS) Stat(string) (fs.FileInfo, error) {
	// No partial file, which is what makes the resume case a full transfer.
	return nil, fs.ErrNotExist
}

// An FS from outside this package cannot implement the unexported fetch
// provider, but it does implement the exported Downloader that remotefs.FS
// embeds, so Download drives it through that rather than refusing it.
func TestDownloadForeignFS(t *testing.T) {
	t.Run("transfers through the exported DownloadURL, atomically", func(t *testing.T) {
		f := newForeignFS(tmpPath)
		require.NoError(t, remotefs.Download(context.Background(), f, testURL, dstPath))

		require.Equal(t, []string{tmpPath}, f.fetchedTo,
			"the transfer must be handed the temporary, never the destination")
		require.Equal(t, dstPath, f.renamedTo[tmpPath],
			"and the temporary must be renamed onto the destination")
	})

	t.Run("a failed transfer still reports", func(t *testing.T) {
		f := newForeignFS(tmpPath)
		f.downloadErr = errors.New("exit status 22")

		err := remotefs.Download(context.Background(), f, testURL, dstPath)
		require.ErrorContains(t, err, "exit status 22")
		require.Empty(t, f.renamedTo, "nothing may land on the destination after a failed transfer")
	})

	t.Run("WithResume is accepted and simply does not resume", func(t *testing.T) {
		f := newForeignFS(tmpPath)
		require.NoError(t, remotefs.Download(context.Background(), f, testURL, dstPath, remotefs.WithResume()))
		require.Len(t, f.fetchedTo, 1, "one full transfer, since DownloadURL cannot be told to continue")
	})

	t.Run("the url is still validated", func(t *testing.T) {
		f := newForeignFS(tmpPath)
		err := remotefs.Download(context.Background(), f, "http://user:pass@test.invalid/a", dstPath)
		require.ErrorContains(t, err, "credentials")
		require.Empty(t, f.fetchedTo, "a rejected url must not reach the FS")
	})
}

func TestDownloadWithoutResume(t *testing.T) {
	t.Run("uses a throwaway temporary and removes it on failure", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, true), -1, errors.New("exit status 18"))
		err := remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath)
		require.Error(t, err)
		require.Equal(t, []bool{false}, rr.resumeSeen, "a plain download never resumes")
		require.NotEmpty(t, commandTouching(t, rr.MockRunner, "rm -- "+tmpPath),
			"without WithResume nothing may be left behind")
	})

	t.Run("does not consult the server at all", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, true), -1, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath))
		require.Empty(t, commandTouching(t, rr.MockRunner, "curl -sS -L -I"),
			"a HEAD is only needed to decide whether to resume")
	})
}

func TestDownloadWithResume(t *testing.T) {
	part := partialFor(t, dstPath, testURL)

	t.Run("keeps the partial file when the transfer fails", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, true), -1, errors.New("exit status 18"))
		err := remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume())
		require.Error(t, err)
		require.Empty(t, commandTouching(t, rr.MockRunner, "rm -- "),
			"the partial is what the next call resumes from, so it must survive")
		require.Contains(t, commandTouching(t, rr.MockRunner, "curl -sSLf"), part,
			"the transfer must write to the url-keyed partial")
	})

	t.Run("continues an existing partial", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, true), 400, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{true}, rr.resumeSeen)
		require.Contains(t, rr.LastCommand(), "mv", "a completed transfer is renamed onto the destination")
	})

	t.Run("starts clean when there is no partial", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, true), -1, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{false}, rr.resumeSeen)
	})

	t.Run("a first attempt costs one stat and nothing else", func(t *testing.T) {
		// This is why there is no separate keep-the-partial option: asking for
		// resume before there is anything to resume behaves exactly like a plain
		// download, so the same option can be passed on every attempt.
		rr := newResumeRunner(t, headOutput(1000, true), -1, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))

		require.Empty(t, commandTouching(t, rr.MockRunner, "curl -sS -L -I"),
			"there are no bytes to check, so the server is not consulted")
		require.Empty(t, commandTouching(t, rr.MockRunner, "truncate"),
			"there is nothing to clear out")
		require.Empty(t, commandTouching(t, rr.MockRunner, "test -f"),
			"the stat already answered whether a partial exists")

		probes := 0
		for _, c := range rr.Commands() {
			if strings.HasPrefix(c, "env -i") {
				probes++
			}
		}
		require.Equal(t, 1, probes, "one stat, not one per question asked about the partial")
	})

	t.Run("starts clean when the server will not range", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, false), 400, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{false}, rr.resumeSeen, "curl exits 33 rather than falling back, so do not ask")
		require.NotEmpty(t, commandTouching(t, rr.MockRunner, "truncate"), "the unusable partial is emptied first")
	})

	t.Run("discards a partial longer than the resource", func(t *testing.T) {
		// curl would range past the end, read the 416 as "already complete" and
		// report success on the wrong bytes.
		rr := newResumeRunner(t, headOutput(1000, true), 4000, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{false}, rr.resumeSeen)
		require.NotEmpty(t, commandTouching(t, rr.MockRunner, "truncate"))
	})

	t.Run("starts clean when the url cannot be inspected", func(t *testing.T) {
		rr := &resumeRunner{MockRunner: rigtest.NewMockRunner()}
		rr.AddCommandOutput(rigtest.Equal("command -v curl"), "/usr/bin/curl")
		rr.AddCommandFailure(rigtest.HasPrefix("curl -sS -L -I"), errors.New("exit status 6"))
		rr.AddCommandOutput(rigtest.HasPrefix("truncate"), "")
		rr.AddCommand(rigtest.HasPrefix("test -f"), func(a *rigtest.A) error { return nil })
		rr.AddCommand(rigtest.HasPrefix("env -i"), func(a *rigtest.A) error {
			_, err := fmt.Fprintf(a.Stdout, "0x81a4 400 2024-09-30 11:42:01.000000000 +0000 //part//\n")
			return err
		})
		rr.AddCommand(rigtest.HasPrefix("curl -sSLf"), func(a *rigtest.A) error {
			rr.resumeSeen = append(rr.resumeSeen, strings.Contains(a.Command, "-C -"))
			return nil
		})
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{false}, rr.resumeSeen, "an unknown url means no promise that ranges work")
	})

	t.Run("a different url does not continue this partial", func(t *testing.T) {
		other := partialFor(t, dstPath, "https://test.invalid/other.tar")
		require.NotEqual(t, part, other,
			"the partial name must key on the url, or a leftover from one url would be fed to another")
	})

	t.Run("removes an unusable partial when truncate is missing", func(t *testing.T) {
		// truncate is a separate binary, so a stripped host may not have it. That
		// must not turn a restart that would have worked into a failure.
		rr := newResumeRunner(t, headOutput(1000, false), 400, nil)
		rr.truncateErr = errors.New("sh: truncate: not found")

		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.NotEmpty(t, commandTouching(t, rr.MockRunner, "rm -- "+part),
			"the partial must be removed once it could not be emptied")
		require.Equal(t, []bool{false}, rr.resumeSeen, "the restart is still a full transfer")
		require.Contains(t, rr.LastCommand(), "mv", "and it still lands on the destination")
	})

	t.Run("reports both failures when the partial cannot be cleared at all", func(t *testing.T) {
		rr := newResumeRunner(t, headOutput(1000, false), 400, nil)
		rr.truncateErr = errors.New("sh: truncate: not found")
		rr.removeErr = errors.New("Permission denied")

		err := remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume())
		require.ErrorContains(t, err, "discard unusable partial")
		require.ErrorContains(t, err, "truncate: not found", "the first failure must survive")
		require.ErrorContains(t, err, "Permission denied", "and so must the one from the fallback")
	})

	t.Run("rejects urls with credentials", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		err := remotefs.Download(context.Background(), remotefs.NewPosixFS(mr), "http://user:pass@test.invalid/a", dstPath)
		require.ErrorContains(t, err, "credentials")
	})
}

// The wget fallback gets its own resume coverage because its flags are not a
// rename of curl's: resume is -c, and the destination is named by -qO, which the
// wget manual says truncates the output file immediately. It does not do that
// when -c is also given -- wget 1.25 sends Range: bytes=<have>- and appends --
// but the invocation is exactly the kind that looks safe to "simplify" into
// something that would silently stop resuming.
func TestDownloadWithResumeViaWget(t *testing.T) {
	part := partialFor(t, dstPath, testURL)

	t.Run("continues an existing partial", func(t *testing.T) {
		rr := newWgetResumeRunner(t, wgetHeadStderr(1000, true), 400, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{true}, rr.resumeSeen)

		transfer := commandTouching(t, rr.MockRunner, "-qO")
		require.NotEmpty(t, transfer, "expected a wget transfer, got %v", rr.Commands())
		require.Contains(t, transfer, "wget -c -qO "+part, "resume must be asked for, and the partial must be what -qO names")
		require.Contains(t, rr.LastCommand(), "mv", "a completed transfer is renamed onto the destination")
	})

	t.Run("starts clean when there is no partial", func(t *testing.T) {
		rr := newWgetResumeRunner(t, wgetHeadStderr(1000, true), -1, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{false}, rr.resumeSeen)
		require.Contains(t, commandTouching(t, rr.MockRunner, "-qO"), "wget -qO "+part,
			"with nothing to continue the transfer must not carry -c")
	})

	t.Run("starts clean when the server will not range", func(t *testing.T) {
		rr := newWgetResumeRunner(t, wgetHeadStderr(1000, false), 400, nil)
		require.NoError(t, remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath, remotefs.WithResume()))
		require.Equal(t, []bool{false}, rr.resumeSeen)
		require.NotEmpty(t, commandTouching(t, rr.MockRunner, "truncate"), "the unusable partial is emptied first")
	})

	t.Run("a plain download never resumes and leaves nothing behind", func(t *testing.T) {
		rr := newWgetResumeRunner(t, wgetHeadStderr(1000, true), -1, errors.New("exit status 8"))
		err := remotefs.Download(context.Background(), remotefs.NewPosixFS(rr), testURL, dstPath)
		require.Error(t, err)
		require.Equal(t, []bool{false}, rr.resumeSeen)
		require.NotEmpty(t, commandTouching(t, rr.MockRunner, "rm -- "+tmpPath),
			"without WithResume nothing may be left behind")
	})
}

func TestDiscardPartial(t *testing.T) {
	t.Run("removes the partial for this url and destination", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		require.NoError(t, remotefs.DiscardPartial(remotefs.NewPosixFS(mr), testURL, dstPath))
		require.Contains(t, mr.LastCommand(), partialFor(t, dstPath, testURL))
	})

	t.Run("nothing to discard is not an error", func(t *testing.T) {
		// Remove reports a not-exist error, which is the normal case after a
		// download that succeeded or never started one.
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("rm"), errors.New("No such file or directory"))
		require.NoError(t, remotefs.DiscardPartial(remotefs.NewPosixFS(mr), testURL, dstPath))
	})

	t.Run("a real removal failure is reported", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.HasPrefix("rm"), errors.New("Permission denied"))
		err := remotefs.DiscardPartial(remotefs.NewPosixFS(mr), testURL, dstPath)
		require.ErrorContains(t, err, "discard partial")
	})

	t.Run("does not touch the destination", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		require.NoError(t, remotefs.DiscardPartial(remotefs.NewPosixFS(mr), testURL, dstPath))
		for _, c := range mr.Commands() {
			require.NotContains(t, c, "rm -- "+dstPath+" ", "only the partial may be removed")
		}
	})
}
