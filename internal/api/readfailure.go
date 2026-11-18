package api

import (
	"errors"
	"net/http"

	"agentplane/internal/store"
)

// codeUnsupportedSchema is the stable error code of a run written by a newer
// build. Clients branch on it; the message is for people.
const codeUnsupportedSchema = "unsupported_schema"

// writeReadFailure answers a failed run read. A run this build cannot decode
// (a newer event schema) is a typed 422 that says so, never a 500 and never the
// misleading "unknown run" it would otherwise look like; any other read failure
// stays a 404 so an ID is never confirmed or denied by its error text.
func writeReadFailure(w http.ResponseWriter, err error) {
	var unsupported *store.UnsupportedSchemaError
	if errors.As(err, &unsupported) {
		writeError(w, http.StatusUnprocessableEntity, codeUnsupportedSchema, unsupported.Error())
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "unknown run")
}
