// Package run holds the vocabulary of one graph run: node and run states and
// their legal transitions, failures and results, approval requests and the
// correlation and feedback values that cross between the engine and node
// executors. It has no behavior beyond invariants of these values, so engine,
// adapters and delivery can share it without depending on one another.
package run
