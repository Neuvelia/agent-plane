package events

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// expectedPayloads maps every event type to the payload the decoder must
// produce for it under the current schema.
var expectedPayloads = map[Type]Payload{
	NodeEvidence: NodeEvidencePayload{}, RunStarted: RunStartedPayload{}, NodeReady: NodeReadyPayload{},
	NodeStarted: NodeStartedPayload{}, LogChunk: LogChunkPayload{}, NodeFinished: NodeFinishedPayload{},
	NodeTerminated: NodeTerminatedPayload{}, RunFinished: RunFinishedPayload{},
	ApprovalRequested: ApprovalRequestedPayload{}, ApprovalDecided: ApprovalDecidedPayload{},
	AttemptStarted: AttemptStartedPayload{}, AttemptInput: AttemptInputPayload{}, AttemptFinished: AttemptFinishedPayload{},
	CancelRequested: CancelRequestedPayload{}, ProcessTerminated: ProcessTerminatedPayload{},
	ExecutorAbandoned: ExecutorAbandonedPayload{Reason: ReasonNodeTimeout},
	AttemptDiff:       AttemptDiffPayload{}, GateCheck: GateCheckPayload{},
	PolicyEvaluated: PolicyEvaluatedPayload{}, PolicyViolation: PolicyViolationPayload{},
	TextDelta: TextDeltaPayload{}, ToolCallStarted: ToolCallPayload{}, ToolCallFinished: ToolCallPayload{},
	CommandStarted: CommandPayload{}, CommandFinished: CommandPayload{}, FileChanged: FileChangedPayload{},
	Status: StatusPayload{}, ProgressOmitted: ProgressOmittedPayload{},
	ReturnEnvelope: ReturnEnvelopePayload{}, DeclaredScopeConflicts: DeclaredScopeConflictsPayload{},
	ValueRecorded:     ValueRecordedPayload{},
	WorkspacePrepared: WorkspacePreparedPayload{}, WorkspaceIntegrated: WorkspaceIntegratedPayload{},
	WorkspaceConflict: WorkspaceConflictPayload{}, WorkspaceReleased: WorkspaceReleasedPayload{},
	SourceWorkspaceChanged: SourceWorkspaceChangedPayload{},
}

// declaredTypeConstants returns the name and value of every constant of type
// Type declared in the non-test sources of this package.
func declaredTypeConstants(t *testing.T) map[string]string {
	t.Helper()
	packages, err := parser.ParseDir(token.NewFileSet(), ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			collectTypeConstants(file, found)
		}
	}
	return found
}

func collectTypeConstants(file *ast.File, found map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != "Type" {
				continue
			}
			for i, name := range value.Names {
				if lit, ok := value.Values[i].(*ast.BasicLit); ok {
					found[name.Name] = strings.Trim(lit.Value, `"`)
				}
			}
		}
	}
}

func TestExpectedPayloadsListEveryDeclaredType(t *testing.T) {
	var problems []string
	declared := map[string]bool{}
	for name, value := range declaredTypeConstants(t) {
		declared[value] = true
		if _, ok := expectedPayloads[Type(value)]; !ok {
			problems = append(problems, "missing entry for "+name)
		}
	}
	for kind := range expectedPayloads {
		if !declared[string(kind)] {
			problems = append(problems, "stale entry "+string(kind))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("expectedPayloads out of sync with declared event types: %v", problems)
	}
}

// customDecoderErrors names the error each payload with its own UnmarshalJSON
// returns for an empty object; those decoders do not report the Go struct name.
var customDecoderErrors = map[Type]string{
	GateCheck:      "check result requires",
	ReturnEnvelope: "return envelope requires",
}

// wrongTypedFieldJSON builds a payload that makes the JSON decoder name the Go
// struct it was decoding into, which identifies the dispatched payload type.
func wrongTypedFieldJSON(t *testing.T, payload Payload) string {
	t.Helper()
	structType := reflect.TypeOf(payload)
	for i := 0; i < structType.NumField(); i++ {
		name := strings.Split(structType.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if structType.Field(i).Type.Kind() == reflect.String {
			return fmt.Sprintf(`{%q:[]}`, name)
		}
		return fmt.Sprintf(`{%q:"x"}`, name)
	}
	t.Fatalf("%T has no JSON field to probe", payload)
	return ""
}

func decodeProbe(t *testing.T, kind Type, payload string) error {
	t.Helper()
	line := fmt.Sprintf(`{"event_schema_version":%d,"type":%q,"payload":%s}`, SchemaVersion, kind, payload)
	var decoded Event
	return json.Unmarshal([]byte(line), &decoded)
}

func TestEveryTypeDecodesIntoItsExpectedPayload(t *testing.T) {
	for kind, payload := range expectedPayloads {
		t.Run(string(kind), func(t *testing.T) {
			if substring, custom := customDecoderErrors[kind]; custom {
				if err := decodeProbe(t, kind, "{}"); err == nil || !strings.Contains(err.Error(), substring) {
					t.Fatalf("type %s did not dispatch to %T: %v", kind, payload, err)
				}
				return
			}
			if reflect.TypeOf(payload).NumField() == 0 {
				assertEmptyPayloadType(t, kind, payload)
				return
			}
			err := decodeProbe(t, kind, wrongTypedFieldJSON(t, payload))
			want := "Go struct field " + reflect.TypeOf(payload).Name() + "."
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("type %s did not dispatch to %T: %v", kind, payload, err)
			}
		})
	}
}

func assertEmptyPayloadType(t *testing.T, kind Type, payload Payload) {
	t.Helper()
	line := fmt.Sprintf(`{"event_schema_version":%d,"type":%q,"payload":{}}`, SchemaVersion, kind)
	var decoded Event
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatal(err)
	}
	if reflect.TypeOf(decoded.Payload) != reflect.TypeOf(payload) {
		t.Fatalf("type %s decoded into %T, want %T", kind, decoded.Payload, payload)
	}
}
