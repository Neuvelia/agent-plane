//go:build race

package store

// raceEnabled is true when the race detector slows the benchmark gate down
// beyond what its budget allows.
const raceEnabled = true
