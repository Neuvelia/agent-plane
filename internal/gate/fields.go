package gate

import "agentplane/internal/fields"

// Kinds the Gate executor accepts. A standalone Gate node offers the first
// three; changed_files_scope belongs to an Agent loop's embedded Gate (graph
// loop rules and docs/graph-spec.md).
const (
	KindCommand           = "command"
	KindJSONPredicate     = "json_predicate"
	KindReportedFileScope = "reported_file_scope"
	KindChangedFilesScope = "changed_files_scope"
)

// diffCommand is the only command a command Gate may run (ADR-0012).
const diffCommand = "/usr/bin/diff"

// ConfigFields describes the Gate configuration parse accepts (F2.2). Each
// kind's keys form a keyless group that applies when kind selects it; parse
// derives its allowed keys from these groups.
func ConfigFields() []fields.Field {
	kindIs := func(k string) []fields.Rule { return []fields.Rule{{Key: "kind", Equals: k}} }
	input := func(id string, types ...string) fields.Field {
		return fields.Field{ID: id, Key: "input", Kind: fields.InputName, Required: true, InputTypes: types, InputRequired: true}
	}
	paths := func(id string) fields.Field {
		return fields.Field{ID: id, Key: "allowed_paths", Kind: fields.PathList, Required: true,
			Item: &fields.Field{ID: id + ".item", Kind: fields.String, Format: fields.FormatRelativePath}}
	}
	return []fields.Field{
		{ID: "gate.config.check_id", Key: "check_id", Kind: fields.String, Required: true, Format: fields.FormatNotBlank, MaxBytes: 256},
		{ID: "gate.config.kind", Key: "kind", Kind: fields.Enum, Required: true,
			Enum: []string{KindCommand, KindJSONPredicate, KindReportedFileScope, KindChangedFilesScope}},
		{ID: "gate.config.command", Kind: fields.Group, Requires: kindIs(KindCommand), Fields: []fields.Field{
			{ID: "gate.config.command.command", Key: "command", Kind: fields.String, Required: true,
				Locked: &fields.Locked{Value: diffCommand, Reason: "a command Gate compares two inputs with " + diffCommand + " only"}},
			{ID: "gate.config.command.cwd", Key: "cwd", Kind: fields.String, Required: true, Format: fields.FormatCleanAbsolutePath},
			{ID: "gate.config.command.left_input", Key: "left_input", Kind: fields.InputName, Required: true, InputTypes: []string{"string"}, InputRequired: true},
			{ID: "gate.config.command.right_input", Key: "right_input", Kind: fields.InputName, Required: true, InputTypes: []string{"string"}, InputRequired: true},
		}},
		{ID: "gate.config.json_predicate", Kind: fields.Group, Requires: kindIs(KindJSONPredicate), Fields: []fields.Field{
			input("gate.config.json_predicate.input", "json"),
			{ID: "gate.config.json_predicate.field", Key: "field", Kind: fields.List, Required: true,
				Item: &fields.Field{ID: "gate.config.json_predicate.field.item", Kind: fields.String, Format: fields.FormatNonEmpty}},
			{ID: "gate.config.json_predicate.operator", Key: "operator", Kind: fields.Enum, Required: true, Enum: []string{"eq", "ne"}},
			{ID: "gate.config.json_predicate.expected", Key: "expected", Kind: fields.Scalar, Required: true,
				Excludes: []fields.Rule{{Key: "expected_json"}}},
			{ID: "gate.config.json_predicate.expected_json", Key: "expected_json", Kind: fields.String, Required: true, Format: fields.FormatJSONScalar,
				Excludes: []fields.Rule{{Key: "expected"}}},
		}},
		{ID: "gate.config.reported_file_scope", Kind: fields.Group, Requires: kindIs(KindReportedFileScope), Fields: []fields.Field{
			input("gate.config.reported_file_scope.input", "json"),
			paths("gate.config.reported_file_scope.allowed_paths"),
		}},
		{ID: "gate.config.changed_files_scope", Kind: fields.Group, Requires: kindIs(KindChangedFilesScope), Fields: []fields.Field{
			input("gate.config.changed_files_scope.input", "string", "json"),
			paths("gate.config.changed_files_scope.allowed_paths"),
			{ID: "gate.config.changed_files_scope.mode", Key: "mode", Kind: fields.Enum, Required: true, Enum: []string{"warn", "deny"}, Default: "warn"},
			{ID: "gate.config.changed_files_scope.workspace", Key: "workspace", Kind: fields.String, Required: true, Format: fields.FormatCleanAbsolutePath},
		}},
	}
}

// OutputPorts lists the outputs a Gate node may declare.
func OutputPorts() []fields.Port {
	return []fields.Port{{Name: "verdict", Type: "string"}}
}
