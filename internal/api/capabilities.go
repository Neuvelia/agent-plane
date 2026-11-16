package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// Capability report routes (F2.11, ADR-0025). Both are read-only: they run the
// composition's bounded detection, keep the result in memory only and never
// start a run, write a file or emit an event.

func (s *Server) getCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.Capabilities == nil {
		writeError(w, http.StatusServiceUnavailable, "capabilities_unavailable", "capability detection is not configured")
		return
	}
	report, err := s.Capabilities.Latest(s.baseCtx, r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "capabilities_unavailable", "capability detection did not finish")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) checkCapabilities(w http.ResponseWriter, r *http.Request) {
	if !readOptionalEmptyObject(w, r) {
		return
	}
	if s.Capabilities == nil {
		writeError(w, http.StatusServiceUnavailable, "capabilities_unavailable", "capability detection is not configured")
		return
	}
	report, err := s.Capabilities.Check(s.baseCtx, r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "capabilities_unavailable", "capability detection did not finish")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// readOptionalEmptyObject accepts an empty body or `{}` and rejects anything
// else, bounded like every other request body.
func readOptionalEmptyObject(w http.ResponseWriter, r *http.Request) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	var empty struct{}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&empty); err != nil || d.More() {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}
