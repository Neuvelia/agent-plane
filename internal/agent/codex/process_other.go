//go:build !darwin && !linux

package codex

import (
	"context"
	"io"
)

func runProcess(context.Context, string, []string, []string, string, io.Reader, io.Writer) error {
	return fail("unsupported_platform", "Codex process ownership requires Darwin or Linux", nil)
}
