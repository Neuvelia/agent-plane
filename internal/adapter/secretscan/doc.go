// Package secretscan looks for granted secret values in the content of named
// workspace files (F3.8 requirement 13, ADR-0029). It is a file adapter only:
// it receives root-relative paths and decides nothing about which paths matter,
// whether a hit is an error or what a run does about it. It reports facts as
// secrets.ScanReport, which carries paths and secret names and never a value.
//
// Paths and file contents are untrusted. The final path component is never
// followed: a symlink's own target text is scanned (as Git stores it), and a
// file is opened with O_NOFOLLOW|O_NONBLOCK and compared with the earlier Lstat
// (SameFile), so a swap to a symlink, FIFO or another file is refused. Ancestors
// are only checked with Lstat before the open, so the scan is best effort
// against a concurrent hostile writer in the same workspace: swapping a
// directory between the check and the open can expose only a path and a secret
// name, never a value. Every file or byte over a limit becomes an explicit gap,
// not a silent skip. The total byte budget is counted per file: a smaller file
// after a refused larger one is still scanned (more coverage than a sticky
// budget, still bounded by the 32 MiB total).
//
// Only unix platforms are supported; elsewhere ScanFiles reports
// secrets.ErrUnobservable. Only cmd/agentplane may import this package.
package secretscan

// Scanner scans workspace files. The zero value is ready to use.
type Scanner struct {
	// afterLstat is a test seam called between the Lstat of a path and its
	// open, to simulate a concurrent swap. It is nil in production.
	afterLstat func(abs string)
}
