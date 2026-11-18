package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

// CheckChangedFiles checks a captured pre-unit Git state without using agent output.
func CheckChangedFiles(ctx context.Context, baseline gitstate.Baseline, allowed []string, mode, checkID string) (events.CheckResult, error) {
	if ctx == nil {
		return events.CheckResult{}, fmt.Errorf("scope check context is nil")
	}
	if mode != "warn" && mode != "deny" {
		return events.CheckResult{}, fmt.Errorf("scope mode must be warn or deny")
	}
	normalized, err := normalizeScopeRules(allowed)
	if err != nil {
		return events.CheckResult{}, err
	}
	paths, err := gitstate.Changed(ctx, baseline)
	if err != nil {
		if ctx.Err() != nil {
			return events.CheckResult{}, ctx.Err()
		}
		if mode == "warn" {
			return indeterminateChangedFiles(normalized, checkID, baseline.Head), nil
		}
		return events.CheckResult{}, err
	}
	p := events.CheckResult{Version: 1, CheckID: checkID, Kind: events.CheckChangedFilesScope, Verdict: events.CheckGreen, Reason: "changed_files_within_scope", Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, ChangedFiles: &events.ChangedFilesScopeDetails{Mode: mode, AllowedPaths: normalized, BaselineHead: baseline.Head, ObservedCount: len(paths)}, Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git"}}}
	var outside []string
	for _, path := range paths {
		if !scopeAllows(normalized, path) {
			outside = append(outside, path)
		}
	}
	if len(outside) > 0 {
		p.Reason = "changed_files_outside_scope"
		if mode == "deny" {
			p.Verdict = events.CheckRed
		}
		p.Evidence = []events.GateEvidence{}
		var omitted uint64
		for _, path := range outside {
			if len(p.Evidence) >= 31 {
				omitted++
				continue
			}
			p.Evidence = append(p.Evidence, events.GateEvidence{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git", Path: path})
			encoded, err := json.Marshal(p)
			if err != nil || len(encoded) > events.MaxGatePayload-1024 {
				p.Evidence = p.Evidence[:len(p.Evidence)-1]
				omitted++
			}
		}
		if omitted > 0 {
			p.Evidence = append(p.Evidence, events.GateEvidence{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git", Omitted: omitted, Reference: &events.EvidenceReference{Kind: "diff", State: "unavailable", Reason: "paths_exceed_evidence_limit"}})
		}
	}
	if err := p.Validate(); err != nil {
		return events.CheckResult{}, err
	}
	return p, nil
}

func normalizeScopeRules(allowed []string) ([]string, error) {
	normalized, err := events.NormalizeUnitScope(allowed)
	if err != nil {
		return nil, err
	}
	for i, rule := range normalized {
		if strings.HasSuffix(rule, "/**") {
			rule = strings.TrimSuffix(rule, "**")
		}
		if strings.ContainsAny(rule, "*?[]") {
			return nil, fmt.Errorf("unsupported scope pattern %q", allowed[i])
		}
		normalized[i] = rule
	}
	sort.Strings(normalized)
	unique := normalized[:0]
	for _, rule := range normalized {
		if len(unique) == 0 || unique[len(unique)-1] != rule {
			unique = append(unique, rule)
		}
	}
	return unique, nil
}

func indeterminateChangedFiles(allowed []string, checkID, head string) events.CheckResult {
	if head == "" {
		head = "unavailable"
	}
	return events.CheckResult{Version: 1, CheckID: checkID, Kind: events.CheckChangedFilesScope, Verdict: events.CheckGreen, Reason: "changed_files_scope_indeterminate", Scope: events.CheckScope{CheckedInputs: []string{"candidate"}}, ChangedFiles: &events.ChangedFilesScopeDetails{Mode: "warn", AllowedPaths: append([]string{}, allowed...), BaselineHead: head}, Evidence: []events.GateEvidence{{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git", Reference: &events.EvidenceReference{Kind: "diff", State: "unavailable", Reason: "git_inspection_failed"}}}}
}

func scopeAllows(allowed []string, path string) bool {
	for _, rule := range allowed {
		if rule == "./" || path == rule || strings.HasSuffix(rule, "/") && strings.HasPrefix(path, rule) {
			return true
		}
	}
	return false
}
