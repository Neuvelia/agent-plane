//go:build !darwin && !linux

package cliproc

import "context"

// Run fails closed: process ownership is implemented for Darwin and Linux only.
func Run(context.Context, Spec) error {
	return newError(StageUnsupported, nil, nil)
}
