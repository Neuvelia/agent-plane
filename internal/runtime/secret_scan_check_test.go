package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/secrets"
)

func identityRedaction(text string) string { return text }

func TestSecretScanCheckBoundsEvidenceAndStaysValid(t *testing.T) {
	var report secrets.ScanReport
	for i := range 400 {
		report.Findings = append(report.Findings, secrets.ScanFinding{Path: fmt.Sprintf("dir/%s/file-%04d.txt", strings.Repeat("x", 200), i), Secret: "gh_token"})
	}
	check := secretScanCheck(scanObservation{report: report}, identityRedaction)
	if err := check.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if check.Verdict != events.CheckRed || check.Reason != "secret_found_in_workspace" {
		t.Fatalf("verdict/reason = %s/%s, want red/secret_found_in_workspace", check.Verdict, check.Reason)
	}
	named, omitted := 0, uint64(0)
	for _, evidence := range check.Evidence {
		if evidence.SecretName != "" {
			named++
		}
		omitted += evidence.Omitted
	}
	if named == 0 || named > 31 || omitted != uint64(len(report.Findings)-named) {
		t.Errorf("named = %d, omitted = %d for %d findings", named, omitted, len(report.Findings))
	}
	if encoded, _ := json.Marshal(check); len(encoded) > events.MaxGatePayload {
		t.Errorf("payload is %d bytes, over the %d bound", len(encoded), events.MaxGatePayload)
	}
}

func TestSecretScanCheckRedactsPathButKeepsSecretName(t *testing.T) {
	const canary = "ghp_PathCanary1Value"
	report := secrets.ScanReport{Findings: []secrets.ScanFinding{{Path: "out/" + canary + ".txt", Secret: "gh_token"}}}
	redact := func(text string) string { return strings.ReplaceAll(text, canary, "***") }
	check := secretScanCheck(scanObservation{report: report}, redact)
	if err := check.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := check.Evidence[0]; got.Path != "out/***.txt" || got.SecretName != "gh_token" {
		t.Errorf("evidence = %+v, want redacted path and kept secret name", got)
	}
}

func TestSecretScanCheckMapsEveryOutcome(t *testing.T) {
	cases := []struct {
		name        string
		observation scanObservation
		reason      string
		reference   string
	}{
		{"clean", scanObservation{report: secrets.ScanReport{Examined: 2}}, "no_secret_in_changed_files", ""},
		{"gaps", scanObservation{report: secrets.ScanReport{Gaps: []secrets.ScanGap{{Reason: secrets.GapFileTooLarge, Count: 2}}}}, "secret_scan_incomplete", "file_too_large"},
		{"no git evidence", scanObservation{err: fmt.Errorf("x: %w", secrets.ErrUnobservable)}, "secret_scan_unobserved", "git_evidence_unavailable"},
		{"other failure", scanObservation{err: errors.New("boom")}, "secret_scan_unobserved", "observation_failed"},
		{"no scanner", scanObservation{err: errScannerNotConfigured}, "secret_scan_unobserved", "scanner_not_configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.observation.duration = time.Millisecond
			check := secretScanCheck(tc.observation, identityRedaction)
			if err := check.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if check.Verdict != events.CheckGreen || check.Reason != tc.reason || check.DurationNS != int64(time.Millisecond) {
				t.Fatalf("check = %s/%s/%d, want green/%s", check.Verdict, check.Reason, check.DurationNS, tc.reason)
			}
			if tc.reference == "" {
				return
			}
			if ref := check.Evidence[0].Reference; ref == nil || ref.Reason != tc.reference {
				t.Errorf("reference = %+v, want reason %s", ref, tc.reference)
			}
		})
	}
}
