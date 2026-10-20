// Package desktop implements the private protocol between the Electron main
// process and its owned local Go service.
package desktop

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const ProtocolVersion uint32 = 1
const MaxFrameBytes = 8 << 20
const MaxInflightRequests = 64

var ErrFrameTooLarge = errors.New("desktop protocol frame exceeds 8 MiB")
var ErrInvalidFrame = errors.New("invalid desktop protocol frame")

type Request struct {
	ProtocolVersion uint32          `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params"`
}

type Response struct {
	ProtocolVersion uint32          `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *ServiceError   `json:"error,omitempty"`
}

type ServiceError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	GraphID   string `json:"graph_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	FieldPath string `json:"field_path,omitempty"`
}

func (e *ServiceError) Error() string { return e.Code + ": " + e.Message }

func protocolError(code, message string) *ServiceError {
	return &ServiceError{Code: code, Message: message}
}

// ReadFrame bounds allocation before reading untrusted payload bytes.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 {
		return nil, ErrInvalidFrame
	}
	if n > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	frame := make([]byte, int(n))
	if _, err := io.ReadFull(r, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

func WriteFrame(w io.Writer, value any) error {
	frame, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(frame) > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(frame)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, frame)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// strictObject rejects duplicate and unknown fields, including fields whose
// value is null. Callers separately check required fields and value types.
func strictObject(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("invalid UTF-8")
	}
	accepted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		accepted[name] = true
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid JSON object")
		}
		key, ok := token.(string)
		if !ok || !accepted[key] || values[key] != nil {
			return nil, errors.New("unknown or duplicate field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("invalid JSON field")
		}
		values[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid JSON object ending")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON data")
	}
	return values, nil
}

func requiredField(fields map[string]json.RawMessage, name string, target any) error {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return fmt.Errorf("missing %s", name)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}

func DecodeRequest(raw []byte) (Request, *ServiceError) {
	var request Request
	fields, err := strictObject(raw, "protocol_version", "request_id", "method", "params")
	if err != nil {
		return request, protocolError("invalid_request", "Invalid request envelope")
	}
	if requiredField(fields, "request_id", &request.RequestID) != nil || !validIdentifier(request.RequestID) {
		return Request{}, protocolError("invalid_request", "Missing or invalid request identity")
	}
	if requiredField(fields, "protocol_version", &request.ProtocolVersion) != nil {
		return request, protocolError("invalid_request", "Missing or invalid protocol version")
	}
	if request.ProtocolVersion != ProtocolVersion {
		return request, protocolError("version_mismatch", "Unsupported protocol version")
	}
	if requiredField(fields, "method", &request.Method) != nil || !validMethod(request.Method) {
		return request, protocolError("invalid_request", "Missing or invalid method")
	}
	request.Params = fields["params"]
	if len(request.Params) == 0 || string(request.Params) == "null" || request.Params[0] != '{' {
		return request, protocolError("invalid_request", "Missing or invalid params")
	}
	return request, nil
}

func validIdentifier(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '!' || id[i] > '~' {
			return false
		}
	}
	return true
}

func validMethod(method string) bool {
	if len(method) < 1 || len(method) > 128 || method[0] < 'a' || method[0] > 'z' {
		return false
	}
	for i := 1; i < len(method); i++ {
		c := method[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '_' {
			return false
		}
	}
	return true
}

// Handler receives params only after framing, envelope, version and handshake
// checks. Each handler must strictly validate its own typed params object.
type Handler func(context.Context, json.RawMessage) (any, *ServiceError)

type Server struct {
	serviceVersion string
	handlers       map[string]Handler
	methods        []string
}

func NewServer(serviceVersion string, handlers map[string]Handler) *Server {
	copyHandlers := make(map[string]Handler, len(handlers))
	methods := []string{"hello"}
	for name, handler := range handlers {
		if name == "hello" || !validMethod(name) || handler == nil {
			panic("invalid desktop protocol handler registration")
		}
		copyHandlers[name] = handler
		methods = append(methods, name)
	}
	sort.Strings(methods)
	return &Server{serviceVersion: serviceVersion, handlers: copyHandlers, methods: methods}
}

func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	stopClose := context.AfterFunc(ctx, func() {
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	defer stopClose()
	var writeMu sync.Mutex
	send := func(response Response) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := WriteFrame(output, response); errors.Is(err, ErrFrameTooLarge) {
			return WriteFrame(output, Response{ProtocolVersion: ProtocolVersion, RequestID: response.RequestID,
				Error: protocolError("response_too_large", "Response exceeds frame limit")})
		} else {
			return err
		}
	}
	var workers sync.WaitGroup
	var fatalMu sync.Mutex
	var fatalErr error
	fatal := func() error {
		fatalMu.Lock()
		defer fatalMu.Unlock()
		return fatalErr
	}
	abort := func(err error) {
		fatalMu.Lock()
		if fatalErr != nil {
			fatalMu.Unlock()
			return
		}
		fatalErr = err
		fatalMu.Unlock()
		cancel()
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	inflight := make(map[string]bool)
	var inflightMu sync.Mutex
	defer func() {
		cancel()
		workers.Wait()
	}()
	ready := false
	for {
		frame, err := ReadFrame(input)
		if workerErr := fatal(); workerErr != nil {
			return workerErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if errors.Is(err, io.EOF) {
			cancel()
			workers.Wait()
			return fatal()
		}
		if err != nil {
			_ = send(Response{ProtocolVersion: ProtocolVersion, RequestID: "", Error: protocolError("invalid_request", "Invalid protocol frame")})
			return err
		}
		request, requestErr := DecodeRequest(frame)
		if requestErr != nil {
			if err := send(Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID, Error: requestErr}); err != nil {
				return err
			}
			continue
		}
		if request.Method == "hello" {
			var response Response
			response.ProtocolVersion, response.RequestID = ProtocolVersion, request.RequestID
			if ready {
				response.Error = protocolError("invalid_request", "Handshake already completed")
			} else if fields, err := strictObject(request.Params, "client_protocol_version"); err != nil {
				response.Error = protocolError("invalid_request", "Invalid handshake params")
			} else {
				var version uint32
				if requiredField(fields, "client_protocol_version", &version) != nil {
					response.Error = protocolError("invalid_request", "Missing client protocol version")
				} else if version != ProtocolVersion {
					response.Error = protocolError("version_mismatch", "Unsupported client protocol version")
				} else {
					ready = true
					response.Result, _ = json.Marshal(struct {
						ProtocolVersion uint32   `json:"protocol_version"`
						ServiceVersion  string   `json:"service_version"`
						Methods         []string `json:"methods"`
					}{ProtocolVersion, s.serviceVersion, s.methods})
				}
			}
			if err := send(response); err != nil {
				return err
			}
			continue
		}
		if !ready {
			if err := send(Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID,
				Error: protocolError("unauthorized", "Handshake required")}); err != nil {
				return err
			}
			continue
		}
		handler, exists := s.handlers[request.Method]
		if !exists {
			if err := send(Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID,
				Error: protocolError("invalid_request", "Unknown method")}); err != nil {
				return err
			}
			continue
		}
		inflightMu.Lock()
		if inflight[request.RequestID] {
			inflightMu.Unlock()
			if err := send(Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID,
				Error: protocolError("conflict", "Request identity already in flight")}); err != nil {
				return err
			}
			continue
		}
		if len(inflight) >= MaxInflightRequests {
			inflightMu.Unlock()
			if err := send(Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID,
				Error: protocolError("busy", "Too many requests in flight")}); err != nil {
				return err
			}
			continue
		}
		inflight[request.RequestID] = true
		inflightMu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				inflightMu.Lock()
				delete(inflight, request.RequestID)
				inflightMu.Unlock()
			}()
			result, serviceErr := handler(ctx, request.Params)
			response := Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID, Error: serviceErr}
			if serviceErr == nil {
				var err error
				response.Result, err = json.Marshal(result)
				if err != nil {
					response.Error = protocolError("internal", "Could not encode response")
				}
			}
			if err := send(response); err != nil {
				abort(err)
			}
		}()
	}
}
