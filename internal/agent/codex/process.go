package codex

import (
	"context"

	"agentplane/internal/agent"
	"agentplane/internal/agent/cliproc"
	"agentplane/internal/proctree"
)

// runProcess runs the Codex CLI through cliproc and maps its classified
// failures to Codex diagnostics. The tree is always owned through proctree.
func runProcess(ctx context.Context, spec cliproc.Spec) error {
	err := cliproc.Run(ctx, spec)
	typed, ok := cliproc.AsError(err)
	if !ok {
		return err
	}
	var out error
	switch typed.Stage {
	case cliproc.StageStart:
		out = fail("start", "cannot start Codex CLI", typed.Cause)
	case cliproc.StageShutdown:
		out = fail("shutdown", "cannot terminate owned process group", typed.Cause)
	case cliproc.StageCancelled:
		out = fail("cancelled", "execution cancelled", typed.Cause)
	default:
		return fail("unsupported_platform", "Codex process ownership requires Darwin or Linux", nil)
	}
	return withTermination(out, typed.Termination)
}

func withTermination(err error, outcome *proctree.Outcome) error {
	if outcome == nil {
		return err
	}
	if typed, ok := err.(*agent.ExecutionError); ok { //nolint:errorlint // fail returns this exact type
		o := *outcome
		typed.Termination = &o
	}
	return err
}
