package api

import (
	"sort"

	"agentplane/internal/adapter/yamlgraph/edit"
	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
)

// Node inspector data (F2.2, ADR-0021). The API does not know the executors:
// the composition root builds the node-type catalogue from the validators'
// descriptors and hands it over in Server.NodeTypes. This file only turns
// that catalogue and a parsed document into the response DTOs.

// NodeType is one node type the sidecar can edit.
type NodeType struct {
	Type          string
	Config        []fields.Field // config descriptors, as the type's validator owns them
	Outputs       []fields.Port  // the outputs the type may declare
	OutputsLocked bool           // outputs are fixed by the engine (human)
	Loop          *LoopType      // nil: the type cannot loop
}

// LoopType describes the loop block a producer type may declare.
type LoopType struct {
	Fields    []fields.Field // the loop's own settings
	Gate      []fields.Field // the embedded Gate's config, offering GateKinds only
	GateKinds []string
}

func (s *Server) nodeType(name string) *NodeType {
	for i := range s.NodeTypes {
		if s.NodeTypes[i].Type == name {
			return &s.NodeTypes[i]
		}
	}
	return nil
}

// configFields serves edit.ApplyWith: the config descriptors of a node type, or of
// its loop Gate, from the catalogue the composition root handed over.
func (s *Server) configFields(nodeType string, loop bool) []fields.Field {
	t := s.nodeType(nodeType)
	switch {
	case t == nil:
		return nil
	case !loop:
		return t.Config
	case t.Loop != nil:
		return t.Loop.Gate
	}
	return nil
}

// nodeTypesFor returns the descriptors for the types used in def, or for every
// known type when all is set.
func (s *Server) nodeTypesFor(def *graph.Definition, all bool) map[string]nodeTypeDTO {
	out := map[string]nodeTypeDTO{}
	for _, t := range s.NodeTypes {
		if all {
			out[t.Type] = toNodeTypeDTO(t)
		}
	}
	if def != nil {
		for _, n := range def.Nodes {
			if t := s.nodeType(n.Type); t != nil {
				out[t.Type] = toNodeTypeDTO(*t)
			}
		}
	}
	return out
}

// nodeInfo returns, per node, its Advanced entries and the nodes it runs
// independently of.
func (s *Server) nodeInfo(text []byte, def *graph.Definition) map[string]nodeInfoDTO {
	snippets := edit.ReadSnippets(text)
	independent := graph.IndependentNodes(def)
	out := make(map[string]nodeInfoDTO, len(def.Nodes))
	for id, n := range def.Nodes {
		info := nodeInfoDTO{IndependentOf: independent[id], Advanced: []advancedDTO{}}
		t := s.nodeType(n.Type)
		info.KnownType = t != nil
		var cfgFields []fields.Field
		if t != nil {
			cfgFields = t.Config
		}
		info.Advanced = advanced(snippets, id, false, cfgFields, n.Config)
		if n.Loop != nil {
			var gateFields []fields.Field
			if t != nil && t.Loop != nil {
				gateFields = t.Loop.Gate
			}
			info.LoopAdvanced = advanced(snippets, id, true, gateFields, n.Loop.Gate.Config)
		}
		out[id] = info
	}
	return out
}

// advanced lists the config keys a form cannot show: keys the descriptors do
// not accept for this config (a key of another Gate kind is one), and values
// the descriptor's control cannot represent. Their text comes from the tree.
func advanced(snippets edit.Snippets, node string, loopGate bool, fs []fields.Field, config map[string]any) []advancedDTO {
	active := map[string]fields.Field{}
	for _, f := range fields.Active(fs, config) {
		active[f.Key] = f
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []advancedDTO{}
	for _, k := range keys {
		reason := ""
		if f, ok := active[k]; !ok {
			reason = "unknown"
		} else if !representable(f, config[k]) {
			reason = "shape"
		}
		if reason == "" {
			continue
		}
		yamlText, _ := snippets.Config(node, loopGate, k)
		out = append(out, advancedDTO{Key: k, YAML: yamlText, Reason: reason})
	}
	return out
}

// representable reports whether a field's control can show v without losing
// it. It is not validation: an empty command or an out-of-range number is
// representable, shown in its control and reported by the validators.
func representable(f fields.Field, v any) bool {
	if v == nil {
		return true // a key without a value is shown empty and reported by validation
	}
	switch f.Kind {
	case fields.String, fields.Text, fields.Enum, fields.InputName:
		_, ok := v.(string)
		return ok
	case fields.Bool:
		_, ok := v.(bool)
		return ok
	case fields.Int:
		_, ok := v.(int)
		return ok
	case fields.Scalar:
		switch v.(type) {
		case string, bool, int:
			return true
		}
		return false
	case fields.List, fields.PathList:
		items, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case fields.Map:
		m, ok := asStringMap(v)
		if !ok {
			return false
		}
		for _, val := range m {
			if _, ok := val.(string); !ok {
				return false
			}
		}
		return true
	case fields.Group:
		_, ok := asStringMap(v)
		return ok
	}
	return false
}

func asStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case graph.NodeConfig:
		return m, true
	}
	return nil, false
}
