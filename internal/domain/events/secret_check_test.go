package events

import "testing"

func secretCheckResult(verdict CheckVerdict, reason string, evidence ...GateEvidence) CheckResult {
	return CheckResult{Version: 1, CheckID: "secret_in_workspace", Kind: CheckSecretInWorkspace, Verdict: verdict, Reason: reason,
		Scope: CheckScope{CheckedInputs: []string{"changed_files"}}, Evidence: evidence}
}

func secretEvidence(path, name string) GateEvidence {
	return GateEvidence{Kind: EvidenceKind(CheckSecretInWorkspace), Source: EvidenceSource(CheckSecretInWorkspace), Path: path, SecretName: name}
}

func omittedSecretEvidence() GateEvidence {
	return GateEvidence{Kind: EvidenceKind(CheckSecretInWorkspace), Source: EvidenceSource(CheckSecretInWorkspace), Omitted: 3,
		Reference: &EvidenceReference{Kind: "diff", State: "unavailable", Reason: "findings_exceed_evidence_limit"}}
}

func TestSecretCheckRejectsIncompleteFindings(t *testing.T) {
	cases := map[string]CheckResult{
		"red without path":        secretCheckResult(CheckRed, "secret_found_in_workspace", secretEvidence("", "gh_token")),
		"red without secret name": secretCheckResult(CheckRed, "secret_found_in_workspace", secretEvidence("leak.txt", "")),
		"red with only omitted":   secretCheckResult(CheckRed, "secret_found_in_workspace", omittedSecretEvidence()),
		"green path without name": secretCheckResult(CheckGreen, "no_secret_in_changed_files", secretEvidence("leak.txt", "")),
		"green secret name":       secretCheckResult(CheckGreen, "no_secret_in_changed_files", secretEvidence("", "gh_token")),
	}
	for name, result := range cases {
		if err := result.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", name, result)
		}
	}
}

func TestSecretCheckAcceptsNamedPathWithOmittedMarker(t *testing.T) {
	red := secretCheckResult(CheckRed, "secret_found_in_workspace", secretEvidence("leak.txt", "gh_token"), omittedSecretEvidence())
	if err := red.Validate(); err != nil {
		t.Fatalf("red Validate: %v", err)
	}
	green := secretCheckResult(CheckGreen, "no_secret_in_changed_files", omittedSecretEvidence())
	if err := green.Validate(); err != nil {
		t.Fatalf("green Validate: %v", err)
	}
}
