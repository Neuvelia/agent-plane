package shell

import "agentplane/internal/fields"

// ConfigFields describes the Shell node configuration parseConfig accepts
// (F2.2). parseConfig derives its allowed keys from it.
func ConfigFields() []fields.Field {
	envName := &fields.Field{ID: "shell.config.env_name", Kind: fields.String, Format: fields.FormatEnvName}
	return []fields.Field{
		{ID: "shell.config.command", Key: "command", Kind: fields.String, Required: true, Format: fields.FormatNonEmpty},
		{ID: "shell.config.args", Key: "args", Kind: fields.List, Item: &fields.Field{ID: "shell.config.args.item", Kind: fields.String}},
		{ID: "shell.config.cwd", Key: "cwd", Kind: fields.String},
		{ID: "shell.config.env_allowlist", Key: "env_allowlist", Kind: fields.List, Item: envName},
		{ID: "shell.config.env", Key: "env", Kind: fields.Map},
		{ID: "shell.config.input_env", Key: "input_env", Kind: fields.Map, InputTypes: []string{"string"}, Item: envName},
		{ID: "shell.config.stdout_json", Key: "stdout_json", Kind: fields.Bool, Default: false},
	}
}

// OutputPorts lists the outputs a Shell node may declare.
func OutputPorts() []fields.Port {
	return []fields.Port{
		{Name: "stdout", Type: "string"},
		{Name: "stderr", Type: "string"},
		{Name: "exit_code", Type: "number"},
		{Name: "json", Type: "json", Requires: []fields.Rule{{Key: "stdout_json", Equals: "true"}}},
	}
}
