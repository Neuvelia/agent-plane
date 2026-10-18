// Package events owns structured execution event contracts.
//
// Events describe observable execution facts in one run-local sequence. The
// in-memory Collector is the F0.6 consumer surface; it has no persistence or
// live subscription semantics. Event builders omit environments, values, and
// raw errors. Shell output is untrusted and passes an optional sanitizer
// before a LogChunk is appended.
package events
