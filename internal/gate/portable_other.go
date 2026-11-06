//go:build !darwin && !linux && !windows

package gate

import (
	"errors"
	"os"
)

type unsupportedComparisonRoot struct{}

func (unsupportedComparisonRoot) Open(string) (*os.File, error) {
	return nil, errors.New("portable read-only comparison requires native safe open on this platform")
}
func (unsupportedComparisonRoot) Verify() error {
	return errors.New("portable read-only comparison requires native safe open on this platform")
}
func (unsupportedComparisonRoot) Close() error { return nil }

func openComparisonRoot(string) (comparisonRoot, error) {
	return nil, errors.New("portable read-only comparison requires native safe open on this platform")
}
