package events

func gateFact() GateCheckPayload {
	return GateCheckPayload{Version: 1, Scope: CheckScope{CheckedInputs: []string{"data"}}, CheckID: "check", Kind: "json_predicate", Verdict: "red", Reason: "mismatch", Evidence: []GateEvidence{{Kind: "json_predicate", Source: "json_predicate"}}}
}
