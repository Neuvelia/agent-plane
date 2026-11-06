//go:build !darwin && !linux && !windows

package process

import "io"

// Native Job Object ownership is required before this fixed mode can run.
func Supervise(io.Reader, io.Writer, io.Writer) int { return 125 }
