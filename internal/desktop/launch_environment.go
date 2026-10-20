package desktop

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"agentplane/internal/graph"
)

const maxLaunchEnvironmentRefs = 64
const maxLaunchEnvironmentBytes = 1 << 20

var ErrLaunchEnvironment = errors.New("required launch environment is unavailable or invalid")

// LaunchEnvironment is a detached snapshot of only the variables explicitly
// named by Agent environment_refs and Shell env_allowlist in one graph.
type LaunchEnvironment struct {
	codex    map[string]string
	shell    map[string]string
	literals []string
}

// CaptureLaunchEnvironment never returns a raw name or value in an error.
// Missing named values block desktop preflight instead of silently changing
// a later process environment.
func CaptureLaunchEnvironment(def *graph.Definition) (LaunchEnvironment, error) {
	if def == nil {
		return LaunchEnvironment{}, ErrLaunchEnvironment
	}
	snapshot := LaunchEnvironment{codex: make(map[string]string), shell: make(map[string]string)}
	unique := make(map[string]string)
	usedBytes := 0
	ids := make([]string, 0, len(def.Nodes))
	for id := range def.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := def.Nodes[id]
		var field string
		var target map[string]string
		switch node.Type {
		case "agent":
			field, target = "environment_refs", snapshot.codex
		case "shell":
			field, target = "env_allowlist", snapshot.shell
		default:
			continue
		}
		refs, err := launchRefList(node.Config[field])
		if err != nil {
			return LaunchEnvironment{}, ErrLaunchEnvironment
		}
		for _, name := range refs {
			if !validLaunchName(name, node.Type) {
				return LaunchEnvironment{}, ErrLaunchEnvironment
			}
			value, ok := os.LookupEnv(name)
			if !ok || strings.ContainsRune(value, 0) || !utf8.ValidString(value) {
				return LaunchEnvironment{}, ErrLaunchEnvironment
			}
			target[name] = value
			if prior, exists := unique[name]; exists {
				if prior != value {
					return LaunchEnvironment{}, ErrLaunchEnvironment
				}
				continue
			}
			unique[name] = value
			usedBytes += len(name) + len(value)
			if len(unique) > maxLaunchEnvironmentRefs || usedBytes > maxLaunchEnvironmentBytes {
				return LaunchEnvironment{}, ErrLaunchEnvironment
			}
		}
		if node.Type == "shell" {
			if raw, present := node.Config["env"]; present {
				literals, err := launchLiteralEnv(raw)
				if err != nil {
					return LaunchEnvironment{}, ErrLaunchEnvironment
				}
				for name, value := range literals {
					snapshot.literals = append(snapshot.literals, value)
					usedBytes += len(name) + len(value)
					if len(unique)+len(snapshot.literals) > maxLaunchEnvironmentRefs || usedBytes > maxLaunchEnvironmentBytes {
						return LaunchEnvironment{}, ErrLaunchEnvironment
					}
				}
			}
		}
	}
	return snapshot, nil
}

func launchLiteralEnv(raw any) (map[string]string, error) {
	result := make(map[string]string)
	switch entries := raw.(type) {
	case map[string]string:
		for name, value := range entries {
			result[name] = value
		}
	case map[string]any:
		for name, value := range entries {
			text, ok := value.(string)
			if !ok {
				return nil, ErrLaunchEnvironment
			}
			result[name] = text
		}
	case graph.NodeConfig:
		for name, value := range entries {
			text, ok := value.(string)
			if !ok {
				return nil, ErrLaunchEnvironment
			}
			result[name] = text
		}
	default:
		return nil, ErrLaunchEnvironment
	}
	for name, value := range result {
		if !validLaunchName(name, "shell") || strings.ContainsRune(value, 0) || !utf8.ValidString(value) {
			return nil, ErrLaunchEnvironment
		}
	}
	return result, nil
}

func launchRefList(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch list := raw.(type) {
	case []string:
		return append([]string(nil), list...), nil
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			value, ok := item.(string)
			if !ok {
				return nil, ErrLaunchEnvironment
			}
			out = append(out, value)
		}
		return out, nil
	default:
		return nil, ErrLaunchEnvironment
	}
}

func validLaunchName(name, nodeType string) bool {
	if name == "" || strings.ContainsAny(name, "=\x00") || !utf8.ValidString(name) {
		return false
	}
	if nodeType == "shell" {
		return true
	}
	if !((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z') || name[0] == '_') {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func cloneLaunchValues(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// CodexValues and ShellValues return non-nil detached maps. A non-nil map
// tells an executor to forbid ambient fallback even when no refs were named.
func (e LaunchEnvironment) CodexValues() map[string]string { return cloneLaunchValues(e.codex) }
func (e LaunchEnvironment) ShellValues() map[string]string { return cloneLaunchValues(e.shell) }

func (e LaunchEnvironment) Redactions() []string {
	unique := make(map[string]bool)
	for _, values := range []map[string]string{e.codex, e.shell} {
		for _, value := range values {
			if value != "" {
				unique[value] = true
			}
		}
	}
	for _, value := range e.literals {
		if value != "" {
			unique[value] = true
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// Seal binds values without exposing them through a public unhashed digest.
// The caller must supply a fresh 32-byte secret kept within this service.
func (e LaunchEnvironment) Seal(key []byte) (string, error) {
	if len(key) != 32 {
		return "", ErrLaunchEnvironment
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("agentplane.launch-environment.v1\x00"))
	for _, group := range []struct {
		kind   string
		values map[string]string
	}{{"codex", e.codex}, {"shell", e.shell}} {
		names := make([]string, 0, len(group.values))
		for name := range group.values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, part := range []string{group.kind, name, group.values[name]} {
				var length [4]byte
				binary.BigEndian.PutUint32(length[:], uint32(len(part)))
				mac.Write(length[:])
				mac.Write([]byte(part))
			}
		}
	}
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}
