package main

import (
	"fmt"
	"io"
	"sort"
)

type severity string

const (
	severityError   severity = "error"
	severityWarning severity = "warning"
)

type finding struct {
	Severity severity
	Location string
	Message  string
}

type findings []finding

func (f *findings) errorf(location, format string, args ...any) {
	*f = append(*f, finding{severityError, location, fmt.Sprintf(format, args...)})
}

func (f *findings) warnf(location, format string, args ...any) {
	*f = append(*f, finding{severityWarning, location, fmt.Sprintf(format, args...)})
}

func (f findings) errorCount() int {
	count := 0
	for _, item := range f {
		if item.Severity == severityError {
			count++
		}
	}
	return count
}

// report prints errors before warnings, each group in location order, so the
// output is stable between runs.
func (f findings) report(out io.Writer) error {
	sorted := append(findings(nil), f...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Severity != sorted[j].Severity {
			return sorted[i].Severity == severityError
		}
		return sorted[i].Location < sorted[j].Location
	})
	for _, item := range sorted {
		if _, err := fmt.Fprintf(out, "%s: %s: %s\n", item.Severity, item.Location, item.Message); err != nil {
			return err
		}
	}
	return nil
}
