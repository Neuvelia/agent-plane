package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const refRoot = "refs/agentplane/"

// idPattern admits only UUID-like identifiers. Run and node-run IDs end up in
// directory and ref names, so they are validated, never sanitized.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// refPattern is the complete set of refs this package will write.
var refPattern = regexp.MustCompile(`^refs/agentplane/[A-Za-z0-9-]{1,64}/(integrated|units/[A-Za-z0-9_-]{1,57})$`)

const (
	maxNodeIDBytes = 4096
	refPrefixBytes = 40
)

func validateID(kind, id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%s %q is not a valid identifier", kind, id)
	}
	return nil
}

// UnitRef names the snapshot commit of one accepted unit. The node ID is
// graph-controlled and may reach 256 bytes (ADR-0015), but a loose ref path
// component is limited by the filesystem to 255 bytes. The name is therefore a
// readable sanitized prefix plus a hash of the raw ID: always valid for Git and
// injective in practice (distinct IDs that sanitize alike still differ).
func UnitRef(runID, nodeID string) (string, error) {
	if err := validateID("run id", runID); err != nil {
		return "", err
	}
	if nodeID == "" || len(nodeID) > maxNodeIDBytes || !utf8.ValidString(nodeID) || strings.ContainsRune(nodeID, 0) {
		return "", errors.New("node id must be 1-4096 bytes of valid UTF-8 without NUL")
	}
	var prefix strings.Builder
	for _, r := range nodeID {
		if prefix.Len() >= refPrefixBytes {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			prefix.WriteRune(r)
		default:
			prefix.WriteByte('_')
		}
	}
	if prefix.Len() == 0 {
		prefix.WriteString("node")
	}
	sum := sha256.Sum256([]byte(nodeID))
	return refRoot + runID + "/units/" + prefix.String() + "-" + hex.EncodeToString(sum[:8]), nil
}

// IntegratedRef names the tip of the run's integration history.
func IntegratedRef(runID string) (string, error) {
	if err := validateID("run id", runID); err != nil {
		return "", err
	}
	return refRoot + runID + "/integrated", nil
}

func validateRef(ref string) error {
	if !refPattern.MatchString(ref) {
		return fmt.Errorf("ref %q is outside refs/agentplane", ref)
	}
	return nil
}
