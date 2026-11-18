//go:build windows

package store

// publishByRename is false: Windows refuses to rename a directory while files in
// it are open, so a run is built in place.
const publishByRename = false

// syncDir is a no-op on Windows: a directory handle cannot be flushed with
// FlushFileBuffers. Lifecycle events rely on File.Sync of the log, which maps to
// FlushFileBuffers, so only the creation of the run directory entry is not
// covered after power loss on this platform.
func syncDir(string) error { return nil }
