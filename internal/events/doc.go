// Package events owns structured execution event contracts.
//
// Events describe observable facts in an authoritative run-local sequence.
// Collector retains lifecycle/evidence and bounded logs/progress; gaps and
// separate live/retention omissions are explicit. Bounded asynchronous live
// delivery is a prefix; terminal facts replay after pump shutdown. No durable
// storage or private reasoning is included. Configured secrets are redacted
// by runtime before progress is stored or delivered.
package events
