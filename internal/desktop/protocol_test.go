package desktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func wireFrame(t *testing.T, payload string) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := binary.Write(&b, binary.BigEndian, uint32(len(payload))); err != nil {
		t.Fatal(err)
	}
	b.WriteString(payload)
	return b.Bytes()
}

func TestFrames(t *testing.T) {
	request := `{"protocol_version":1,"request_id":"r1","method":"hello","params":{"client_protocol_version":1}}`
	got, err := ReadFrame(bytes.NewReader(wireFrame(t, request)))
	if err != nil || string(got) != request {
		t.Fatalf("read = %s, %v", got, err)
	}
	for _, input := range [][]byte{wireFrame(t, request)[:5], {0, 0, 0, 4, '{'}} {
		if _, err := ReadFrame(bytes.NewReader(input)); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("short frame error = %v", err)
		}
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0, 128, 0, 1})); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized frame error = %v", err)
	}
	var output bytes.Buffer
	if err := WriteFrame(&output, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), wireFrame(t, `{"ok":true}`)) {
		t.Fatalf("written frame = %q", output.Bytes())
	}
	if err := WriteFrame(io.Discard, strings.Repeat("x", MaxFrameBytes)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("write oversized frame error = %v", err)
	}
}

func TestDecodeRequestRejectsInvalidEnvelope(t *testing.T) {
	valid := `{"protocol_version":1,"request_id":"r1","method":"hello","params":{"client_protocol_version":1}}`
	if _, err := DecodeRequest([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRequest([]byte(`{ "protocol_version": 1, "request_id": "r2", "method": "hello", "params": { "client_protocol_version": 1 } }`)); err != nil {
		t.Fatalf("valid whitespace request rejected: %v", err)
	}
	cases := []string{
		`{"protocol_version":2,"request_id":"r1","method":"hello","params":{}}`,
		`{"protocol_version":1,"request_id":"r1","method":"hello","params":{},"extra":1}`,
		`{"protocol_version":1,"request_id":"r1","request_id":"r2","method":"hello","params":{}}`,
		`{"protocol_version":1,"request_id":null,"method":"hello","params":{}}`,
		`{"protocol_version":1,"request_id":"r1","method":"hello"}`,
		`{"protocol_version":1,"request_id":"r1","method":"hello","params":null}`,
		`{"protocol_version":1,"request_id":"r1","method":"hello","params":{}} trailing`,
		`{"protocol_version":1,"request_id":"\n","method":"hello","params":{}}`,
	}
	for _, input := range cases {
		if _, err := DecodeRequest([]byte(input)); err == nil {
			t.Errorf("accepted invalid request %q", input)
		}
	}
	if _, err := DecodeRequest([]byte{0xff}); err == nil {
		t.Error("accepted invalid UTF-8")
	}
	request, err := DecodeRequest([]byte(`{"protocol_version":2,"request_id":"version-request","method":"hello","params":{}}`))
	if err == nil || err.Code != "version_mismatch" || request.RequestID != "version-request" {
		t.Fatalf("version response identity = %q, %v", request.RequestID, err)
	}
	for _, input := range []string{
		`{"protocol_version":1,"request_id":"valid-id","method":"bad-method!","params":{}}`,
		`{"protocol_version":1,"request_id":"valid-id","method":"hello","params":null}`,
	} {
		request, err := DecodeRequest([]byte(input))
		if err == nil || request.RequestID != "valid-id" {
			t.Fatalf("lost error identity: %q, %v", request.RequestID, err)
		}
	}
}

func TestServerHandshakeAndMethods(t *testing.T) {
	server := NewServer("test-version", map[string]Handler{
		"settings.get": func(context.Context, json.RawMessage) (any, *ServiceError) {
			return struct {
				Revision int `json:"revision"`
			}{1}, nil
		},
	})
	input := bytes.Join([][]byte{
		wireFrame(t, `{"protocol_version":1,"request_id":"before","method":"settings.get","params":{}}`),
		wireFrame(t, `{"protocol_version":1,"request_id":"hello","method":"hello","params":{"client_protocol_version":1}}`),
		wireFrame(t, `{"protocol_version":1,"request_id":"unknown","method":"shell.exec","params":{}}`),
		wireFrame(t, `{"protocol_version":1,"request_id":"settings","method":"settings.get","params":{}}`),
	}, nil)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := readResponses(t, &output, 4)
	if responses["before"].Error.Code != "unauthorized" || responses["unknown"].Error.Code != "invalid_request" {
		t.Fatalf("errors = %#v", responses)
	}
	if responses["hello"].Error != nil || !bytes.Contains(responses["hello"].Result, []byte(`"settings.get"`)) {
		t.Fatalf("hello = %#v", responses["hello"])
	}
	if string(responses["settings"].Result) != `{"revision":1}` {
		t.Fatalf("settings = %s", responses["settings"].Result)
	}
}

func TestServerRequiresExactHandshake(t *testing.T) {
	server := NewServer("test", nil)
	input := bytes.Join([][]byte{
		wireFrame(t, `{"protocol_version":1,"request_id":"bad","method":"hello","params":{"client_protocol_version":1,"extra":true}}`),
		wireFrame(t, `{"protocol_version":1,"request_id":"mismatch","method":"hello","params":{"client_protocol_version":2}}`),
		wireFrame(t, `{"protocol_version":1,"request_id":"good","method":"hello","params":{"client_protocol_version":1}}`),
	}, nil)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := readResponses(t, &output, 3)
	if responses["bad"].Error.Code != "invalid_request" || responses["mismatch"].Error.Code != "version_mismatch" || responses["good"].Error != nil {
		t.Fatalf("handshake responses = %#v", responses)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestServerReportsClosedOutput(t *testing.T) {
	server := NewServer("test", nil)
	input := bytes.NewReader(wireFrame(t, `{"protocol_version":1,"request_id":"hello","method":"hello","params":{"client_protocol_version":1}}`))
	if err := server.Serve(context.Background(), input, failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("closed output error = %v", err)
	}
}

type failAfterHelloWriter struct{ writes int }

func (w *failAfterHelloWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes > 2 {
		return 0, io.ErrClosedPipe
	}
	return len(b), nil
}

func TestServerStopsOnHandlerOutputFailure(t *testing.T) {
	server := NewServer("test", map[string]Handler{
		"settings.get": func(context.Context, json.RawMessage) (any, *ServiceError) { return struct{}{}, nil },
	})
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, &failAfterHelloWriter{}) }()
	for _, request := range []string{
		`{"protocol_version":1,"request_id":"hello","method":"hello","params":{"client_protocol_version":1}}`,
		`{"protocol_version":1,"request_id":"read","method":"settings.get","params":{}}`,
	} {
		if _, err := writer.Write(wireFrame(t, request)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("output failure = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service kept reading after output failure")
	}
}

func TestServerContextCancellationUnblocksInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- NewServer("test", nil).Serve(ctx, reader, io.Discard) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not unblock protocol input")
	}
}

func TestServerCancelsHandlerWhenParentPipeCloses(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	server := NewServer("test", map[string]Handler{
		"wait": func(ctx context.Context, _ json.RawMessage) (any, *ServiceError) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return struct{}{}, nil
		},
	})
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, io.Discard) }()
	for _, request := range []string{
		`{"protocol_version":1,"request_id":"hello","method":"hello","params":{"client_protocol_version":1}}`,
		`{"protocol_version":1,"request_id":"wait","method":"wait","params":{}}`,
	} {
		if _, err := writer.Write(wireFrame(t, request)); err != nil {
			t.Fatal(err)
		}
	}
	<-started
	writer.Close()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("handler survived closed parent pipe")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service did not exit after pipe closure")
	}
}

func TestServerRejectsDuplicateInflightID(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := NewServer("test", map[string]Handler{
		"wait": func(context.Context, json.RawMessage) (any, *ServiceError) {
			close(entered)
			<-release
			return struct{}{}, nil
		},
	})
	reader, writer := io.Pipe()
	output, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), reader, outputWriter); outputWriter.Close() }()
	if _, err := writer.Write(wireFrame(t, `{"protocol_version":1,"request_id":"hello","method":"hello","params":{"client_protocol_version":1}}`)); err != nil {
		t.Fatal(err)
	}
	if response := nextResponse(t, output); response.RequestID != "hello" || response.Error != nil {
		t.Fatalf("handshake = %#v", response)
	}
	if _, err := writer.Write(wireFrame(t, `{"protocol_version":1,"request_id":"same","method":"wait","params":{}}`)); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := writer.Write(wireFrame(t, `{"protocol_version":1,"request_id":"same","method":"wait","params":{}}`)); err != nil {
		t.Fatal(err)
	}
	if response := nextResponse(t, output); response.RequestID != "same" || response.Error == nil || response.Error.Code != "conflict" {
		t.Fatalf("duplicate response = %#v", response)
	}
	close(release)
	writer.Close()
	if response := nextResponse(t, output); response.RequestID != "same" || response.Error != nil {
		t.Fatalf("original response = %#v", response)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func readResponses(t *testing.T, source io.Reader, count int) map[string]Response {
	t.Helper()
	result := make(map[string]Response)
	for i := 0; i < count; i++ {
		response := nextResponse(t, source)
		if (response.Result == nil) == (response.Error == nil) {
			t.Fatalf("invalid response union: %#v", response)
		}
		result[response.RequestID] = response
	}
	return result
}

func nextResponse(t *testing.T, source io.Reader) Response {
	t.Helper()
	frame, err := ReadFrame(source)
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(frame, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"protocol_version":1,"request_id":"r1","method":"hello","params":{}}`))
	f.Add([]byte{0xff, 0, '{'})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = DecodeRequest(b) })
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Add([]byte{0, 128, 0, 1})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = ReadFrame(bytes.NewReader(b)) })
}
