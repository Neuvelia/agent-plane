package runtime

import (
	"strings"
	"testing"

	"agentplane/internal/domain/events"
)

func TestAttemptDiffRunBudgetOmitsLaterFilesAsBudget(t *testing.T) {
	r := &diffRecorder{budget: 100}
	file := func(path string, patch int) events.AttemptDiffPayload {
		return events.AttemptDiffPayload{Path: path, Change: events.DiffAdded, AddedLines: 3, Patch: strings.Repeat("x", patch)}
	}
	result := &attemptDiffResult{capture: AttemptCapture{
		Files:   []events.AttemptDiffPayload{file("a", 60), file("b", 60), file("c", 30)},
		Summary: events.AttemptDiffPayload{Files: 3, PatchBytes: 150, AddedLines: 9},
	}}
	files, summary := r.fit(result)
	if files[0].Patch == "" || files[1].Patch != "" || files[1].OmittedReason != events.DiffOmitBudget || files[1].AddedLines != 3 || files[2].Patch == "" {
		t.Fatalf("files = %+v", files)
	}
	if summary.PatchBytes != 90 || summary.Files != 3 || r.budget != 10 {
		t.Fatalf("summary=%+v budget=%d", summary, r.budget)
	}
	if err := files[1].Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptDiffInvalidAdapterOutputBecomesSummaryOnly(t *testing.T) {
	r := &diffRecorder{budget: 100}
	files, summary := r.fit(&attemptDiffResult{capture: AttemptCapture{Files: []events.AttemptDiffPayload{{Path: "/abs", Change: events.DiffAdded}}}})
	if len(files) != 0 || summary.DiagnosticCode != events.DiffGitFailed || summary.Validate() != nil {
		t.Fatalf("files=%v summary=%+v", files, summary)
	}
}
