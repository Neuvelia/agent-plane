//go:build !darwin && !linux

package codex

import (
	"context"
	"io"

	"agentplane/internal/proctree"
)

func runProcess(context.Context, string, []string, []string, string, io.Reader, io.Writer, proctree.Policy) error {
	return fail("unsupported_platform", "Codex process ownership requires Darwin or Linux", nil)
}
