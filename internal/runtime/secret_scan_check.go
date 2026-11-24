package runtime

import (
	"errors"
	"sort"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/secrets"
)

// secretScanCheckID is the check_id of the node-run scoped Check Result that
// reports the workspace scan (F3.8 requirement 13).
const secretScanCheckID = "secret_in_workspace"

// maxSecretFindingEvidence leaves one of the 32 evidence slots for the marker
// that counts the findings that did not fit.
const maxSecretFindingEvidence = 31

// errScannerNotConfigured records that a granted node ran in a runner without a
// WorkspaceScanner. It is an observation gap, never a silent skip.
var errScannerNotConfigured = errors.New("no workspace scanner configured")

// scanObservation is everything the Check Result is derived from. It holds
// paths and secret names only, never a secret value.
type scanObservation struct {
	report   secrets.ScanReport
	err      error
	duration time.Duration
}

// secretScanCheck maps one observation to its Check Result: red only for a
// finding; green with a distinct reason for clean, incomplete and unobserved.
// redact is applied to every path, because a path is untrusted text that may
// itself spell a secret (an agent can name a file after the value).
func secretScanCheck(observation scanObservation, redact func(string) string) events.GateCheckPayload {
	switch {
	case observation.err != nil:
		return unobservedCheck(observation, unobservedReason(observation.err))
	case len(observation.report.Findings) > 0:
		return foundCheck(observation, redact)
	case len(observation.report.Gaps) > 0:
		return incompleteCheck(observation)
	default:
		return secretCheck(observation, events.CheckGreen, "no_secret_in_changed_files", []events.GateEvidence{secretEvidence()})
	}
}

func secretCheck(observation scanObservation, verdict events.CheckVerdict, reason string, evidence []events.GateEvidence) events.GateCheckPayload {
	return events.GateCheckPayload{
		Version: 1, CheckID: secretScanCheckID, Kind: events.CheckSecretInWorkspace, Verdict: verdict, Reason: reason,
		Scope: events.CheckScope{CheckedInputs: []string{"changed_files"}}, DurationNS: observation.duration.Nanoseconds(), Evidence: evidence,
	}
}

func secretEvidence() events.GateEvidence {
	return events.GateEvidence{Kind: events.EvidenceKind(events.CheckSecretInWorkspace), Source: events.EvidenceSource(events.CheckSecretInWorkspace)}
}

func unavailableEvidence(reason string, omitted uint64) events.GateEvidence {
	evidence := secretEvidence()
	evidence.Reference = &events.EvidenceReference{Kind: "diff", State: "unavailable", Reason: reason}
	evidence.Omitted = omitted
	return evidence
}

func unobservedCheck(observation scanObservation, reason string) events.GateCheckPayload {
	return secretCheck(observation, events.CheckGreen, "secret_scan_unobserved", []events.GateEvidence{unavailableEvidence(reason, 0)})
}

func unobservedReason(err error) string {
	switch {
	case errors.Is(err, errScannerNotConfigured):
		return "scanner_not_configured"
	case errors.Is(err, secrets.ErrUnobservable):
		return "git_evidence_unavailable"
	default:
		return "observation_failed"
	}
}

func incompleteCheck(observation scanObservation) events.GateCheckPayload {
	evidence := make([]events.GateEvidence, 0, len(observation.report.Gaps))
	for _, gap := range observation.report.Gaps {
		evidence = append(evidence, unavailableEvidence(string(gap.Reason), gap.Count))
	}
	return secretCheck(observation, events.CheckGreen, "secret_scan_incomplete", evidence)
}

// foundCheck keeps as many findings as fit the payload budget and counts the
// rest. A red result is never dropped: if even one finding cannot be encoded,
// the scan degrades to unobserved rather than to green.
func foundCheck(observation scanObservation, redact func(string) string) events.GateCheckPayload {
	findings := redactedFindings(observation.report.Findings, redact)
	for keep := min(len(findings), maxSecretFindingEvidence); keep > 0; keep /= 2 {
		check := secretCheck(observation, events.CheckRed, "secret_found_in_workspace", findingEvidence(findings, keep))
		if check.Validate() == nil {
			return check
		}
	}
	return unobservedCheck(observation, "observation_failed")
}

func redactedFindings(findings []secrets.ScanFinding, redact func(string) string) []secrets.ScanFinding {
	redacted := make([]secrets.ScanFinding, 0, len(findings))
	for _, finding := range findings {
		redacted = append(redacted, secrets.ScanFinding{Path: redact(finding.Path), Secret: finding.Secret})
	}
	sort.SliceStable(redacted, func(i, j int) bool {
		if redacted[i].Path != redacted[j].Path {
			return redacted[i].Path < redacted[j].Path
		}
		return redacted[i].Secret < redacted[j].Secret
	})
	return redacted
}

func findingEvidence(findings []secrets.ScanFinding, keep int) []events.GateEvidence {
	evidence := make([]events.GateEvidence, 0, keep+1)
	for _, finding := range findings[:keep] {
		entry := secretEvidence()
		entry.Path, entry.SecretName = finding.Path, finding.Secret
		evidence = append(evidence, entry)
	}
	if keep < len(findings) {
		evidence = append(evidence, unavailableEvidence("findings_exceed_evidence_limit", uint64(len(findings)-keep)))
	}
	return evidence
}

// pathRedactor returns a function that replaces every value the run knows
// (granted secrets, their encoded forms and --redact-env values) in a path.
func pathRedactor(template *logRedactor) func(string) string {
	return func(text string) string {
		c := template.clone()
		out := append([]byte{}, c.Write(events.Stdout, []byte(text))...)
		return string(append(out, c.Flush(events.Stdout)...))
	}
}
