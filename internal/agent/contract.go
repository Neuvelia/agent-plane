// Package agent defines the provider-neutral coding-agent execution boundary.
package agent

import (
	"context"
	"encoding/json"

	"agentplane/internal/events"
)

type AdapterID string

// AgentAdapter is implemented by a coding-agent provider outside execution-core.
// Execute must honor ctx and wrap its cancellation cause when cancellation ends work.
type AgentAdapter interface {
	ID() AdapterID
	Detect(context.Context) (CapabilityReport, error)
	Execute(context.Context, AgentExecutionInput) (AgentExecutionOutput, error)
}

// Optional capability flags use nil for unknown and non-nil false for known lack of support.
type CapabilityReport struct {
	Available      bool    `json:"available"`
	DisplayName    string  `json:"display_name,omitempty"`
	Version        *string `json:"version,omitempty"`
	Cancellation   *bool   `json:"cancellation,omitempty"`
	Streaming      *bool   `json:"streaming,omitempty"`
	UsageReporting *bool   `json:"usage_reporting,omitempty"`
	Workspace      *bool   `json:"workspace,omitempty"`
}

// AgentExecutionInput contains only explicitly authored references, not resolved
// files, environment values, secrets, or hidden repository context.
type AgentExecutionInput struct {
	Task            string           `json:"task"`
	Workspace       string           `json:"workspace"`
	AllowedPaths    []string         `json:"allowed_paths,omitempty"`
	ContextRefs     []string         `json:"context_refs,omitempty"`
	EnvironmentRefs []string         `json:"environment_refs,omitempty"`
	RunID           events.RunID     `json:"run_id,omitempty"`
	NodeRunID       events.NodeRunID `json:"node_run_id,omitempty"`
	AttemptID       events.AttemptID `json:"attempt_id,omitempty"`
}

type FileProvenance string

const (
	ProvenanceReported FileProvenance = "reported"
	ProvenanceObserved FileProvenance = "observed"
)

type ChangedFile struct {
	Path       string         `json:"path"`
	Provenance FileProvenance `json:"provenance"`
}

type ArtifactReference struct {
	Ref  string `json:"ref"`
	Name string `json:"name,omitempty"`
}

// Nil fields distinguish unknown usage from provider-reported zero.
type UsageMetadata struct {
	InputTokens  *int64  `json:"input_tokens,omitempty"`
	OutputTokens *int64  `json:"output_tokens,omitempty"`
	ModelName    *string `json:"model_name,omitempty"`
	ProviderCost *string `json:"provider_cost,omitempty"`
	Currency     *string `json:"currency,omitempty"`
}

type AgentExecutionOutput struct {
	Summary      string              `json:"summary,omitempty"`
	ChangedFiles []ChangedFile       `json:"changed_files,omitempty"`
	Artifacts    []ArtifactReference `json:"artifacts,omitempty"`
	// RawMetadata is validated JSON that execution-core passes through without interpretation.
	// Adapters must not intentionally put secrets in it.
	RawMetadata json.RawMessage `json:"raw_metadata,omitempty"`
	Usage       *UsageMetadata  `json:"usage,omitempty"`
}

// ProgressAdapter reports only safe observable statuses; the base adapter remains compatible.
type ProgressAdapter interface {
	ExecuteWithProgress(context.Context, AgentExecutionInput, func(string, []byte)) (AgentExecutionOutput, error)
}
