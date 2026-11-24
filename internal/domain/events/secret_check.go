package events

import "errors"

// validSecretCheck enforces the evidence contract of F3.8 requirement 13: a red
// result names the path and the declared secret of at least one finding, and
// path and secret name always travel together. Entries without either (the
// omitted-findings marker, the clean and unobserved diagnostics) stay valid. The
// value of a secret is never a field of the evidence.
func validSecretCheck(p GateCheckPayload) error {
	if p.Kind != CheckSecretInWorkspace {
		return nil
	}
	named := 0
	for _, e := range p.Evidence {
		if (e.Path == "") != (e.SecretName == "") {
			return errors.New("secret_in_workspace evidence needs both a path and a secret_name, or neither")
		}
		if e.SecretName != "" {
			named++
		}
	}
	if p.Verdict == CheckRed && named == 0 {
		return errors.New("a red secret_in_workspace result must name a path and a secret")
	}
	if p.Verdict == CheckGreen && named > 0 {
		return errors.New("a green secret_in_workspace result cannot name a secret")
	}
	return nil
}
