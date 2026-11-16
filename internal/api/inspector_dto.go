package api

import "agentplane/internal/fields"

// Inspector DTOs (F2.2). They copy the descriptors field by field so that the
// wire format is written here, not inherited from internal/fields.

type ruleDTO struct {
	Key    string `json:"key,omitempty"`
	Equals string `json:"equals,omitempty"`
	Input  string `json:"input,omitempty"`
}

type lockedDTO struct {
	Value  any    `json:"value"`
	Reason string `json:"reason"`
}

type fieldDTO struct {
	ID            string     `json:"id"`
	Key           string     `json:"key,omitempty"`
	Kind          string     `json:"kind"`
	Required      bool       `json:"required,omitempty"`
	Default       any        `json:"default,omitempty"`
	Enum          []string   `json:"enum,omitempty"`
	Min           *int       `json:"min,omitempty"`
	Max           *int       `json:"max,omitempty"`
	MaxBytes      int        `json:"max_bytes,omitempty"`
	MaxItems      int        `json:"max_items,omitempty"`
	Format        string     `json:"format,omitempty"`
	Item          *fieldDTO  `json:"item,omitempty"`
	InputTypes    []string   `json:"input_types,omitempty"`
	InputRequired bool       `json:"input_required,omitempty"`
	Fields        []fieldDTO `json:"fields,omitempty"`
	Requires      []ruleDTO  `json:"requires,omitempty"`
	Excludes      []ruleDTO  `json:"excludes,omitempty"`
	Locked        *lockedDTO `json:"locked,omitempty"`
}

type outputPortDescriptorDTO struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Required bool      `json:"required,omitempty"`
	Requires []ruleDTO `json:"requires,omitempty"`
}

type loopTypeDTO struct {
	Fields    []fieldDTO `json:"fields"`
	Gate      []fieldDTO `json:"gate"`
	GateKinds []string   `json:"gate_kinds"`
}

type nodeTypeDTO struct {
	Config        []fieldDTO                `json:"config"`
	Outputs       []outputPortDescriptorDTO `json:"outputs"`
	OutputsLocked bool                      `json:"outputs_locked"`
	Loop          *loopTypeDTO              `json:"loop,omitempty"`
}

// advancedDTO is a config key the form cannot show. Reason is "unknown" (the
// descriptors do not accept the key for this config) or "shape" (the value
// does not fit the field's control). YAML is the value's text from the tree.
type advancedDTO struct {
	Key    string `json:"key"`
	YAML   string `json:"yaml"`
	Reason string `json:"reason"`
}

type nodeInfoDTO struct {
	KnownType     bool          `json:"known_type"`
	Advanced      []advancedDTO `json:"advanced"`
	LoopAdvanced  []advancedDTO `json:"loop_advanced,omitempty"`
	IndependentOf []string      `json:"independent_of"`
}

func toNodeTypeDTO(t NodeType) nodeTypeDTO {
	out := nodeTypeDTO{Config: toFieldDTOs(t.Config), Outputs: []outputPortDescriptorDTO{}, OutputsLocked: t.OutputsLocked}
	for _, p := range t.Outputs {
		out.Outputs = append(out.Outputs, outputPortDescriptorDTO{Name: p.Name, Type: p.Type, Required: p.Required, Requires: toRuleDTOs(p.Requires)})
	}
	if t.Loop != nil {
		out.Loop = &loopTypeDTO{Fields: toFieldDTOs(t.Loop.Fields), Gate: toFieldDTOs(t.Loop.Gate), GateKinds: append([]string{}, t.Loop.GateKinds...)}
	}
	return out
}

func toFieldDTOs(fs []fields.Field) []fieldDTO {
	out := make([]fieldDTO, 0, len(fs))
	for _, f := range fs {
		out = append(out, toFieldDTO(f))
	}
	return out
}

func toFieldDTO(f fields.Field) fieldDTO {
	d := fieldDTO{
		ID: f.ID, Key: f.Key, Kind: string(f.Kind), Required: f.Required, Default: f.Default,
		Enum: append([]string(nil), f.Enum...), Min: f.Min, Max: f.Max, MaxBytes: f.MaxBytes, MaxItems: f.MaxItems,
		Format: string(f.Format), InputTypes: append([]string(nil), f.InputTypes...), InputRequired: f.InputRequired,
		Requires: toRuleDTOs(f.Requires), Excludes: toRuleDTOs(f.Excludes),
	}
	if f.Item != nil {
		item := toFieldDTO(*f.Item)
		d.Item = &item
	}
	if len(f.Fields) > 0 {
		d.Fields = toFieldDTOs(f.Fields)
	}
	if f.Locked != nil {
		d.Locked = &lockedDTO{Value: f.Locked.Value, Reason: f.Locked.Reason}
	}
	return d
}

func toRuleDTOs(rs []fields.Rule) []ruleDTO {
	if len(rs) == 0 {
		return nil
	}
	out := make([]ruleDTO, len(rs))
	for i, r := range rs {
		out[i] = ruleDTO{Key: r.Key, Equals: r.Equals, Input: r.Input}
	}
	return out
}
