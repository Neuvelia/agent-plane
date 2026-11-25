package graph

import (
	"fmt"
	"strings"
)

// Diagnostics reach the terminal, logs and API responses before the F3.8
// guards look for resolved secret values, so they never echo author text that
// failed the identifier grammar: a value pasted in place of a key could be a
// secret. Text that matches the grammar is graph structure that events already
// carry and is quoted as usual.

// invalidKeyText stands in for author text that is not shown.
const invalidKeyText = "<invalid identifier>"

// quotedKey quotes key when it matches the identifier grammar and returns
// invalidKeyText otherwise.
func quotedKey(key string) string {
	if validIdentifier(key) {
		return fmt.Sprintf("%q", key)
	}
	return invalidKeyText
}

// mapPath addresses key under prefix. An invalid key is not echoed, so the
// path names the enclosing collection.
func mapPath(prefix, key string) string {
	if validIdentifier(key) {
		return prefix + "." + key
	}
	return prefix
}

// safeLocation drops a node id that fails the grammar and, for a path inside
// that node, points at the nodes collection instead.
func safeLocation(location DiagnosticLocation) DiagnosticLocation {
	if location.NodeID == "" || validIdentifier(location.NodeID) {
		return location
	}
	location.NodeID = ""
	if location.Path == "nodes" || strings.HasPrefix(location.Path, "nodes.") || strings.HasPrefix(location.Path, "nodes[") {
		location.Path = "nodes"
	}
	return location
}

// fieldPath addresses field of the entry key under prefix, or only the
// enclosing collection when key is invalid.
func fieldPath(prefix, key, field string) string {
	if validIdentifier(key) {
		return prefix + "." + key + "." + field
	}
	return prefix
}
