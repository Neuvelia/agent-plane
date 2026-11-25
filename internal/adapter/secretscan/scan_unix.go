//go:build unix

package secretscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"agentplane/internal/domain/secrets"
)

// readChunk is how often a long read checks for cancellation.
const readChunk = 1 << 20

// ScanFiles looks for the raw value of every target in the files named by paths
// (slash-separated, relative to root). Paths are deduplicated and sorted; the
// first limits.Files are examined and the rest are a file_limit_reached gap.
// Each file is read whole into one buffer, so a value can never straddle a
// window boundary. A canceled ctx aborts with ctx.Err() and no report. Deleted
// paths are skipped without a gap: they have no content to leak.
func (sc Scanner) ScanFiles(ctx context.Context, root string, paths []string, targets []secrets.ScanTarget, limits secrets.ScanLimits) (secrets.ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return secrets.ScanReport{}, err
	}
	if len(targets) == 0 {
		return secrets.ScanReport{}, nil
	}
	if err := limits.Validate(); err != nil {
		return secrets.ScanReport{}, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return secrets.ScanReport{}, fmt.Errorf("resolve scan root: %w", err)
	}
	run := &scanRun{afterLstat: sc.afterLstat, root: realRoot, targets: targets, limits: limits, gaps: map[secrets.ScanGapReason]uint64{}}
	ordered := orderedPaths(paths)
	if len(ordered) > limits.Files {
		run.gaps[secrets.GapFileLimitReached] = uint64(len(ordered) - limits.Files)
		ordered = ordered[:limits.Files]
	}
	for _, rel := range ordered {
		if err := run.scanPath(ctx, rel); err != nil {
			return secrets.ScanReport{}, err
		}
	}
	return run.report(), nil
}

func orderedPaths(paths []string) []string {
	ordered := slices.Clone(paths)
	slices.Sort(ordered)
	return slices.Compact(ordered)
}

// scanRun is the state of one ScanFiles call; it is never shared.
type scanRun struct {
	afterLstat func(abs string)
	root       string
	targets    []secrets.ScanTarget
	limits     secrets.ScanLimits
	used       int64
	buf        []byte
	findings   []secrets.ScanFinding
	gaps       map[secrets.ScanGapReason]uint64
	examined   uint64
}

func (r *scanRun) scanPath(ctx context.Context, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !confinedPath(rel) {
		r.gaps[secrets.GapUnreadable]++
		return nil
	}
	switch checkAncestors(r.root, rel) {
	case ancestorsMissing:
		return nil
	case ancestorsRefused:
		r.gaps[secrets.GapUnreadable]++
		return nil
	}
	abs := filepath.Join(r.root, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		r.gaps[secrets.GapUnreadable]++
		return nil
	case info.Mode()&fs.ModeSymlink != 0:
		r.scanLinkText(abs, rel)
		return nil
	case !info.Mode().IsRegular():
		r.gaps[secrets.GapNotRegularFile]++
		return nil
	}
	return r.scanRegular(ctx, abs, rel, info)
}

// scanLinkText examines the text a symlink stores, which Git records as the
// entry's content. The link is never followed.
func (r *scanRun) scanLinkText(abs, rel string) {
	text, err := os.Readlink(abs)
	if err != nil {
		r.gaps[secrets.GapUnreadable]++
		return
	}
	if int64(len(text)) > r.limits.FileBytes {
		text = text[:r.limits.FileBytes]
	}
	r.examine(rel, []byte(text))
}

func (r *scanRun) scanRegular(ctx context.Context, abs, rel string, lstat fs.FileInfo) error {
	if r.refuseBySize(lstat.Size()) {
		return nil
	}
	if r.afterLstat != nil {
		r.afterLstat(abs)
	}
	file, err := openNoFollow(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.gaps[secrets.GapUnreadable]++
		return nil
	}
	defer func() { _ = file.Close() }() // read-only: a close failure loses nothing
	opened, err := file.Stat()
	// The mode is checked before identity so a non-regular file is classified by
	// what was opened, not by whether the filesystem reused the inode number.
	switch {
	case err != nil:
		r.gaps[secrets.GapUnreadable]++
		return nil
	case !opened.Mode().IsRegular():
		r.gaps[secrets.GapNotRegularFile]++
		return nil
	case !os.SameFile(lstat, opened):
		r.gaps[secrets.GapUnreadable]++
		return nil
	}
	data, err := r.read(ctx, file)
	if err != nil {
		return r.readFailure(ctx)
	}
	if r.refuseBySize(int64(len(data))) {
		r.used += int64(len(data)) // a file that grew was still read
		return nil
	}
	r.examine(rel, data)
	return nil
}

// refuseBySize records a gap and reports true when size cannot be scanned
// within the per-file or the remaining total budget.
func (r *scanRun) refuseBySize(size int64) bool {
	switch {
	case size > r.limits.FileBytes:
		r.gaps[secrets.GapFileTooLarge]++
	case r.used+size > r.limits.TotalBytes:
		r.gaps[secrets.GapTotalBudgetReached]++
	default:
		return false
	}
	return true
}

func (r *scanRun) readFailure(ctx context.Context) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	r.gaps[secrets.GapUnreadable]++
	return nil
}

// read returns at most FileBytes+1 bytes so a file that grew past the limit
// after Lstat is detected without reading its tail. The returned slice aliases
// the run's reusable buffer and is valid until the next read.
func (r *scanRun) read(ctx context.Context, file *os.File) ([]byte, error) {
	limit := int(min(r.limits.FileBytes+1, r.limits.TotalBytes-r.used+1))
	data := r.buf[:0]
	for len(data) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		want := min(readChunk, limit-len(data))
		data = slices.Grow(data, want)
		n, err := file.Read(data[len(data) : len(data)+want])
		data = data[:len(data)+n]
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	r.buf = data
	return data, nil
}

func (r *scanRun) examine(rel string, data []byte) {
	if r.used+int64(len(data)) > r.limits.TotalBytes {
		r.gaps[secrets.GapTotalBudgetReached]++
		return
	}
	r.used += int64(len(data))
	r.examined++
	for _, name := range secrets.Match(data, r.targets) {
		r.findings = append(r.findings, secrets.ScanFinding{Path: rel, Secret: name})
	}
}

func (r *scanRun) report() secrets.ScanReport {
	sort.Slice(r.findings, func(i, j int) bool {
		if r.findings[i].Path != r.findings[j].Path {
			return r.findings[i].Path < r.findings[j].Path
		}
		return r.findings[i].Secret < r.findings[j].Secret
	})
	var gaps []secrets.ScanGap
	for reason, count := range r.gaps {
		gaps = append(gaps, secrets.ScanGap{Reason: reason, Count: count})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].Reason < gaps[j].Reason })
	return secrets.ScanReport{Findings: r.findings, Gaps: gaps, Examined: r.examined}
}
