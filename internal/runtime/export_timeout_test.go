package runtime

import "time"

// SetTimeoutForTest shortens the authored-seconds unit and the abandon bound
// for external-package tests and returns a restore function.
func SetTimeoutForTest(unit, bound time.Duration) func() {
	oldUnit, oldBound := timeoutUnit, abandonBound
	timeoutUnit, abandonBound = unit, bound
	return func() { timeoutUnit, abandonBound = oldUnit, oldBound }
}
