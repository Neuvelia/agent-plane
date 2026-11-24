// Package secretscan looks for granted secret values in the content of named
// workspace files (F3.8 requirement 13, ADR-0029). It is a file adapter only:
// it receives root-relative paths and decides nothing about which paths matter,
// whether a hit is an error or what a run does about it. It reports facts as
// secrets.ScanReport, which carries paths and secret names and never a value.
//
// Paths and file contents are untrusted. Symlinks are never followed (a
// symlink's own target text is scanned, as Git stores it), path ancestors are
// checked with Lstat, files are opened with O_NOFOLLOW|O_NONBLOCK and compared
// with the earlier Lstat so a swap to a symlink or FIFO is refused, and every
// file or byte over a limit becomes an explicit gap, not a silent skip.
//
// Only unix platforms are supported; elsewhere ScanFiles reports
// secrets.ErrUnobservable. Only cmd/agentplane may import this package.
package secretscan

// Scanner scans workspace files. The zero value is ready to use.
type Scanner struct{}
