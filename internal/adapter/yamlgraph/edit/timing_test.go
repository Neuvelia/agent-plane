package edit

import (
	"testing"
	"time"

	"agentplane/internal/adapter/yamlgraph"
)

// The speed tests of this package compare wall-clock time with a limit. Wall
// clock measures how busy the machine is as much as how slow the code is: with
// two other CPU-bound processes on a four-core host every sample of a 200-node
// graph is 1.5x slower, with four it is 3-4x slower, and taking more samples
// does not help because the load lasts longer than all of them. A real
// regression slows the operation relative to the work the machine does for
// everything else, so each test also compares its best sample with the time to
// parse the same document in the same process, and fails only when both the
// absolute limit and that ratio are exceeded.
//
// The ratio bounds below were chosen from measurements of best-of-9 samples on
// the 200-node fixtures of these tests (four vCPUs, from idle to four busy
// loops), and are about 1.6x above the largest ratio seen:
//
//	operation                     observed op/parse    bound
//	add_node (TestLargeGraph...)        6.3 - 7.1       12
//	list_connections                   17.5 - 24.9      40
//
// A slowdown that stays below the bound on an idle machine (for list_connections
// about 2.3x) is therefore not reported by these tests; they exist to catch an
// algorithmic blow-up on a large graph, not a small constant-factor change.
const (
	addNodeParseRatioBound         = 12.0
	listConnectionsParseRatioBound = 40.0
)

// bestOf returns the shortest of samples runs of f. A scheduling pause makes a
// single sample slow without the code being slow, so the best of several is the
// least noisy estimate of the cost of f.
func bestOf(samples int, f func()) time.Duration {
	best := time.Duration(1<<63 - 1)
	for i := 0; i < samples; i++ {
		start := time.Now()
		f()
		if d := time.Since(start); d < best {
			best = d
		}
	}
	return best
}

// parseTime is the best time to parse src, the reference every speed test
// scales its result with. It is measured with the same number of samples as
// the operation so both see the same kind of noise.
func parseTime(t *testing.T, src []byte, samples int) time.Duration {
	t.Helper()
	var failure error
	best := bestOf(samples, func() {
		if _, err := yamlgraph.Parse(src); err != nil {
			failure = err
		}
	})
	if failure != nil {
		t.Fatal(failure)
	}
	return best
}

// withinBudget reports whether best is acceptable: it is below limit, or it is
// not more than maxRatio times the time to parse the same document, which means
// the machine is slow rather than the operation.
func withinBudget(best, limit, parse time.Duration, maxRatio float64) bool {
	return best <= limit || float64(best) <= maxRatio*float64(parse)
}

func TestWithinBudgetFailsOnlyWhenBothBoundsAreExceeded(t *testing.T) {
	const (
		limit = 500 * time.Millisecond
		parse = 20 * time.Millisecond
		ratio = 40.0
	)
	cases := []struct {
		name string
		best time.Duration
		want bool
	}{
		{"under the limit", 300 * time.Millisecond, true},
		{"exactly at the limit", limit, true},
		{"slow machine: over the limit but in proportion to parse", 790 * time.Millisecond, true},
		{"at the ratio bound", 800 * time.Millisecond, true},
		{"regression: over the limit and out of proportion", 801 * time.Millisecond, false},
		{"large regression", 3 * time.Second, false},
	}
	for _, tc := range cases {
		if got := withinBudget(tc.best, limit, parse, ratio); got != tc.want {
			t.Errorf("%s: withinBudget(%v) = %v, want %v", tc.name, tc.best, got, tc.want)
		}
	}
}

func TestWithinBudgetRejectsAnOperationWhenParseIsFastButTheOperationIsNot(t *testing.T) {
	// A fast machine (parse 5 ms) must still fail an operation that exceeds the
	// absolute limit: the ratio alone would allow up to 40 x 5 ms = 200 ms only,
	// so 600 ms is a regression.
	if withinBudget(600*time.Millisecond, 500*time.Millisecond, 5*time.Millisecond, 40) {
		t.Fatal("a 600 ms operation next to a 5 ms parse was accepted")
	}
}
