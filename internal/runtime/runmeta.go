package runtime

import (
	"context"
	"time"

	"agentplane/internal/domain/events"
)

// RunMetadata is the optional provenance recorded in run_started.
type RunMetadata struct {
	RepoName, RepoID, RepoBranch, RepoCommit, RuntimeVersion string
}

// metadataBound is the most time run_started waits for its provenance (F2.9,
// F3.6). The provider runs a few local Git queries; a slow repository loses its
// provenance rather than delaying the run.
const metadataBound = 2 * time.Second

// collectMetadata calls the provider defensively: a panic, a slow provider or
// one that ignores its context yields empty metadata after metadataBound, never
// a failed or stalled run. The provider's goroutine owns no run state; its
// context is cancelled when the wait ends, so a provider that honours it stops.
// A field the event contract would reject is dropped (wellFormed), because
// run_started must never fail on best-effort provenance.
func collectMetadata(ctx context.Context, provider func(context.Context) RunMetadata) RunMetadata {
	bounded, cancel := context.WithTimeout(ctx, metadataBound)
	defer cancel()
	result := make(chan RunMetadata, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- RunMetadata{}
			}
		}()
		result <- provider(bounded)
	}()
	select {
	case meta := <-result:
		return wellFormed(meta)
	case <-bounded.Done():
		return RunMetadata{}
	}
}

// wellFormed keeps each field that the run_started contract accepts on its own
// and blanks the rest, for example a repository folder named with a backslash.
func wellFormed(meta RunMetadata) RunMetadata {
	keep := func(field string, payload events.RunStartedPayload) string {
		if events.CheckRunProvenance(payload) != nil {
			return ""
		}
		return field
	}
	return RunMetadata{
		RepoName:       keep(meta.RepoName, events.RunStartedPayload{RepoName: meta.RepoName}),
		RepoID:         keep(meta.RepoID, events.RunStartedPayload{RepoID: meta.RepoID}),
		RepoBranch:     keep(meta.RepoBranch, events.RunStartedPayload{RepoBranch: meta.RepoBranch}),
		RepoCommit:     keep(meta.RepoCommit, events.RunStartedPayload{RepoCommit: meta.RepoCommit}),
		RuntimeVersion: keep(meta.RuntimeVersion, events.RunStartedPayload{RuntimeVersion: meta.RuntimeVersion}),
	}
}
