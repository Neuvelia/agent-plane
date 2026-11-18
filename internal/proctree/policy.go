package proctree

import (
	"fmt"
	"time"
)

// DefaultGrace is the graceful-stop window used when Policy.Grace is zero.
const DefaultGrace = 5 * time.Second

// verifyBound limits how long Terminate waits for a forced tree to empty.
const verifyBound = time.Second

// Policy tunes one Terminate call. The zero value selects DefaultGrace and no
// second-interrupt channel.
type Policy struct {
	Grace    time.Duration   // zero selects DefaultGrace
	Escalate <-chan struct{} // closed on a second interrupt: skip the rest of the grace
}

func (p Policy) grace() time.Duration {
	if p.Grace <= 0 {
		return DefaultGrace
	}
	return p.Grace
}

// Method records how a tree ended: a graceful stop within the grace, a forced
// kill after it, or a tree that was already gone.
type Method string

// Method values reported by Terminate.
const (
	MethodGraceful      Method = "graceful"
	MethodForced        Method = "forced"
	MethodAlreadyExited Method = "already_exited"
)

// Outcome describes one Terminate call.
type Outcome struct {
	Method  Method
	Grace   time.Duration // configured
	Elapsed time.Duration // request to verified empty
	Leaked  int           // members still alive after force; exact on Linux, otherwise at least 1
}

// LeakError reports a tree that could not be emptied.
type LeakError struct{ Leaked int }

func (e *LeakError) Error() string {
	return fmt.Sprintf("process tree not empty after forced termination: %d process(es) alive", e.Leaked)
}
