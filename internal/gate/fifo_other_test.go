//go:build !darwin && !linux

package gate

import "testing"

func makeFIFO(t *testing.T, _ string) {
	t.Helper()
	t.Skip("Unix FIFO does not exist on this platform")
}
