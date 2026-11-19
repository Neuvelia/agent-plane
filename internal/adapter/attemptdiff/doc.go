// Package attemptdiff captures what one Agent correction attempt changed in a
// Git workspace (F2.8b, ADR-0026). It takes content-aware snapshots of the
// working tree through the hardened gitstate.Run and compares two of them into
// bounded, redacted attempt_diff payloads. It reports facts; the runtime owns
// when to snapshot, the run budget and event order, and capture never decides
// execution.
//
// Git is only asked for ls-files, cat-file blob and (through gitstate) rev-parse.
// Nothing is written into the repository; dirty or untracked before-content is
// copied to a private scratch directory that Close removes.
package attemptdiff
