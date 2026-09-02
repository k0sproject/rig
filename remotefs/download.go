package remotefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
)

// ErrDownloadNotSupported is returned by the DownloadURL methods when the FS
// implementation cannot fetch URLs, so a caller can tell that apart from a
// transfer that was attempted and failed.
//
// [Download] never raises it itself, since every remotefs.FS exposes DownloadURL
// for it to fall back to, but it does propagate one raised by the FS it was
// given -- so testing for it with errors.Is stays meaningful there.
var ErrDownloadNotSupported = errors.New("download not supported by this filesystem type")

// urlFetcher is implemented by FS types that can fetch a URL into a given path.
// The resume flag asks for an interrupted transfer to that same path to be
// continued instead of restarted; implementations that cannot resume fall back
// to a full transfer, which is always correct, just slower.
//
// It is the preferred path rather than the only one: it is what carries the
// context and the resume flag, but [fetcherFor] falls back to the exported
// DownloadURL so that [Download] works with any FS, not just this package's.
type urlFetcher interface {
	fetchURL(ctx context.Context, url, dst string, resume bool) error
}

// [downloadToTemp] reaches the implementations through a type assertion, so a
// drifting signature would silently lose the capability rather than fail to
// compile.
var (
	_ urlFetcher = (*PosixFS)(nil)
	_ urlFetcher = (*WinFS)(nil)
)

// downloadToTemp fetches into a temporary file alongside dst and moves it onto
// dst only once fetch reports success, so dst is never observed half-written.
//
// The temporary lives in the same directory as dst so the move is a rename
// within one filesystem rather than a copy across two. It is removed if the
// fetch fails; a hard crash can still strand one, which is the cost of never
// leaving a truncated file at the destination.
//
// fetch receives the temporary path and may be called more than once with it.
func downloadToTemp(ctx context.Context, fsys FS, dst string, fetch func(ctx context.Context, tmp string) error) error {
	tmp, err := fsys.CreateTemp(fsys.Dir(dst), fsys.Base(dst)+".")
	if err != nil {
		return fmt.Errorf("download %s: create temporary file: %w", dst, err)
	}

	if err := fetch(ctx, tmp); err != nil {
		// The destination is untouched, so the partial file has nothing left to
		// say. A removal failure must not mask the download failure.
		if rmErr := fsys.Remove(tmp); rmErr != nil {
			return fmt.Errorf("%w (leaving %s behind: %w)", err, tmp, rmErr)
		}
		return err
	}

	if err := fsys.Rename(tmp, dst); err != nil {
		if rmErr := fsys.Remove(tmp); rmErr != nil {
			return fmt.Errorf("download %s: rename from %s: %w (leaving it behind: %w)", dst, tmp, err, rmErr)
		}
		return fmt.Errorf("download %s: rename from %s: %w", dst, tmp, err)
	}
	return nil
}

// fetcherFor returns how Download transfers url for fsys.
//
// The unexported provider is preferred, being the only path that honours a
// context and can resume. Any other FS is still driven through DownloadURL,
// which is all that remotefs.FS guarantees, so Download is not restricted to
// this package's own implementations -- it just cannot cancel or resume on one.
// Atomicity survives either way, since the move needs only exported methods.
func fetcherFor(fsys FS, url string) func(ctx context.Context, dst string, resume bool) error {
	if f, ok := fsys.(urlFetcher); ok {
		return func(ctx context.Context, dst string, resume bool) error {
			return f.fetchURL(ctx, url, dst, resume)
		}
	}
	return func(_ context.Context, dst string, _ bool) error {
		return fsys.DownloadURL(url, dst)
	}
}

// downloadURL is the shared implementation behind the DownloadURL methods. It
// keeps their signature and their acceptance of any URL string while routing
// the transfer through a temporary file.
//
// This one cannot take [fetcherFor]'s fallback: it *is* what DownloadURL calls,
// so reaching for the exported method would recurse. The compile-time
// assertions above are what keep the guard from ever firing.
func downloadURL(fsys FS, url, dst string) error {
	f, ok := fsys.(urlFetcher)
	if !ok {
		return fmt.Errorf("download %s: %w", url, ErrDownloadNotSupported)
	}
	return downloadToTemp(context.Background(), fsys, dst, func(ctx context.Context, tmp string) error {
		return f.fetchURL(ctx, url, tmp, false)
	})
}

// downloadOptions carries the tunables for [Download].
type downloadOptions struct {
	resume bool
}

// DownloadOption configures [Download].
type DownloadOption func(*downloadOptions)

// WithResume keeps the partially transferred file when a download fails, and
// continues it on the next [Download] call for the same url and destination
// instead of starting over. Retrying is left to the caller; this only makes a
// retry cheap.
//
// The partial is kept beside the destination under a name derived from both the
// destination and the url, so a later call can only continue a file that its own
// url and destination produced. Without this option a failed download leaves
// nothing behind.
//
// Resuming assumes the bytes already on disk are a prefix of what the url serves
// now. That holds for an interrupted transfer of unchanging content, which is
// what versioned artifact URLs provide. It does not hold if the content behind
// the url is replaced between attempts: neither curl nor wget can detect that,
// and the result would be half of each version.
//
// To catch that before spending a transfer on it, record the ETag or
// LastModified that [HTTPHead] reports for the url alongside the download,
// compare it before retrying, and call [DiscardPartial] when it has changed. A
// checksum over the finished file catches what no header can say.
//
// Windows hosts always restart the transfer: Invoke-WebRequest under Windows
// PowerShell 5.1 cannot continue one. So does an FS from outside this package,
// which can only be driven through its exported DownloadURL. The partial is
// still kept and the result is still correct, so the option is safe to pass in
// both cases, it just saves nothing.
func WithResume() DownloadOption {
	return func(o *downloadOptions) {
		o.resume = true
	}
}

// size returns the size of name, or zero when it cannot be determined.
func size(fsys FS, name string) int64 {
	info, err := fsys.Stat(name)
	if err != nil {
		return 0
	}
	return info.Size()
}

// partialPath is where an interrupted download of url to dst is parked.
//
// The url is folded into the name so that a partial can only ever be continued
// by a call carrying the same url and destination. That is what makes resuming
// across separate calls safe to offer: the alternative, a name derived from the
// destination alone, would happily continue a partial left by a different url.
func partialPath(fsys FS, dst, url string) string {
	sum := sha256.Sum256([]byte(url))
	return fsys.Join(fsys.Dir(dst), fsys.Base(dst)+".rigpart-"+hex.EncodeToString(sum[:6]))
}

// resumable reports whether the bytes already at the partial file can be
// continued, and is deliberately conservative: anything it cannot confirm means
// starting over, which is always correct and only costs time.
func resumable(ctx context.Context, fsys FS, url string, have int64) bool {
	if have <= 0 {
		return false
	}
	info, err := HTTPHead(ctx, fsys, url)
	if err != nil {
		// Nothing is known about the url, including whether ranges work.
		return false
	}
	if !info.AcceptRanges {
		// curl refuses with exit 33 against a server that will not range, and
		// would leave the transfer stranded rather than fall back.
		return false
	}
	if info.ContentLength >= 0 && have > info.ContentLength {
		// Longer than the whole resource, so it cannot be a prefix of it. Left
		// alone, curl would ask for a range past the end, take the 416 as "this
		// is already complete" and report success on the wrong bytes.
		return false
	}
	return true
}

// Download fetches url to dst on the remote host.
//
// The transfer goes to a file beside dst and is renamed onto it only once it
// completes, so dst is never observed half-written.
//
// Downloads are not retried here. A failed call returns its error and the caller
// decides whether to try again; see [WithResume] for making that retry continue
// where the last attempt stopped rather than starting over.
//
// An FS from outside this package is transferred through its exported
// DownloadURL, so it still gets the atomic move and the url validation, but
// neither cancellation nor resume: both need the richer internal provider that
// PosixFS and WinFS implement.
func Download(ctx context.Context, fsys FS, url, dst string, opts ...DownloadOption) error {
	if err := validateHTTPURL(url); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	var options downloadOptions
	for _, opt := range opts {
		opt(&options)
	}

	transfer := fetcherFor(fsys, url)

	if !options.resume {
		return downloadToTemp(ctx, fsys, dst, func(ctx context.Context, tmp string) error {
			return transfer(ctx, tmp, false)
		})
	}

	part := partialPath(fsys, dst, url)
	// One stat answers both questions: whether there is anything to continue,
	// and whether there is anything to clear out. With no partial yet -- every
	// first attempt -- that is the only extra work this option costs, since
	// resumable does not reach for a HEAD when there are no bytes to check.
	have := size(fsys, part)
	resume := resumable(ctx, fsys, url, have)
	if !resume && have > 0 {
		// Whatever is there cannot be built on. Emptying it is preferred over
		// removing it, as it keeps the path stable for the attempt about to write
		// it and needs no write permission on the directory. But truncate is a
		// separate binary that a stripped host may not carry, and its absence
		// must not turn a workable from-scratch restart into a failure, so a
		// removal stands in: curl and wget both create the file they are given.
		if err := fsys.Truncate(part, 0); err != nil {
			if rmErr := fsys.Remove(part); rmErr != nil {
				return fmt.Errorf("download %s: discard unusable partial %s: %w (removing it instead: %w)", url, part, err, rmErr)
			}
		}
	}
	if err := transfer(ctx, part, resume); err != nil {
		// Deliberately kept: it is what the next call resumes from.
		return err
	}
	if err := fsys.Rename(part, dst); err != nil {
		return fmt.Errorf("download %s: rename %s to %s: %w", url, part, dst, err)
	}
	return nil
}

// DiscardPartial removes the partially transferred file that a [Download] with
// [WithResume] leaves behind for this url and destination.
//
// Call it when abandoning a download for good. The partial is deliberately kept
// across failures so that a later attempt can continue it, which means nothing
// else will ever remove it, and an abandoned one holds its bytes on the
// destination's filesystem indefinitely. For a large artifact that is the whole
// download sitting there unreferenced.
//
// Having nothing to discard is not an error, so it is safe to call after a
// download that succeeded or never started one.
func DiscardPartial(fsys FS, url, dst string) error {
	part := partialPath(fsys, dst, url)
	if err := fsys.Remove(part); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("discard partial %s: %w", part, err)
	}
	return nil
}
