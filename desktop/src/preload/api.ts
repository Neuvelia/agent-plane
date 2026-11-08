import type { EventPage, RunSnapshot } from '../renderer/runState'

export type TypedInput =
  | { type: 'string'; value: string }
  | { type: 'number'; value: string }
  | { type: 'boolean'; value: boolean }
  | { type: 'json'; value: unknown }
  | { type: 'artifact_ref'; value: string }

export type RunSelection = {
  document_id: string
  revision: number
  workspace_id: string
  inputs: Record<string, TypedInput>
  max_concurrency: number
}

export type StartSelection = RunSelection & {
  idempotency_key: string
  preflight_token: string
  payload_digest: string
}

export type StartResult = {
  run_id: string
  state: string
  graph_version: string
  trace_watermark: number
  history_availability?: 1 | 2 | 3
}

export type CurrentRun = {
  state: 'none' | 'pending' | 'known' | 'unknown'
  run_id?: string
}

export type DocumentView = {
  document_id: string
  project_id: string
  relative_path: string
  text: string
  revision: number
  disk_digest: string
  validity: 'valid' | 'invalid'
  semantic_version?: string
  [key: string]: unknown
}

export type Availability = 'ready' | 'missing' | 'inaccessible'
export type ProjectView = { project_id: string; display_name: string; canonical_path: string; availability: Availability }
export type WorkspaceView = { workspace_id: string; project_id: string; relative_path: string; availability: Availability }
export type ProjectPage = { projects: ProjectView[]; next_cursor?: string }
export type WorkspacePage = { workspaces: WorkspaceView[]; next_cursor?: string }
export type LocalSettings = {
  revision: number; theme: string; codex_binary_path?: string; git_binary_path?: string
  max_concurrency: number; history_retention_days?: number; keep_all_history: boolean
}
export type ToolStatus = { tool_id: 'git' | 'codex'; status: string; path?: string; diagnostic?: string }
export type Diagnostic = {
  code: string; severity: string; message: string; graph_id?: string; node_id?: string
  edge_index?: number; graph_input_binding_index?: number; path?: string
  line?: number; column?: number; suggestion?: string
}
export type DiagnosticSummary = {
  diagnostics: Diagnostic[]; diagnostics_total: number; diagnostics_omitted: number; has_errors: boolean
}
export type PreflightResult = DiagnosticSummary & {
  preflight_token?: string; graph_version?: string; payload_digest?: string
}
export type RunCancelResult = { run_id: string; state: string }

export type HistoryRunSummary = {
  run_id: string; project_id: string; graph_id: string; started_at: string
  ended_at?: string; storage_state: 'active' | 'complete' | 'interrupted'
  runtime_state?: string; pinned: boolean; revision: number
  completeness: unknown; completeness_digest: string
}
export type HistoryListPage = { runs: HistoryRunSummary[]; next_cursor?: string; generation: number }
export type HistoryRun = Omit<HistoryRunSummary, 'completeness' | 'completeness_digest'> & {
  workspace_id: string; document_id: string; graph_hash: string
}
export type HistoryDetail = {
  run: HistoryRun; graph: unknown; graph_digest: string; launch: unknown; launch_digest: string
  result?: unknown; result_digest?: string; completeness: unknown; completeness_digest: string
}
export type HistoryNodeOutcome = { node_run_id: string; node_id: string; status: string; projection: unknown; digest: string }
export type HistoryAttemptOutcome = { attempt_id: string; node_run_id: string; status: string; projection: unknown; digest: string }
export type HistoryOutcomePage = {
  nodes: HistoryNodeOutcome[]; attempts: HistoryAttemptOutcome[]
  next_node_cursor?: string; next_attempt_cursor?: string
}
export type HistoryEventPage = {
  events: { sequence: number; projection: unknown; digest: string }[]
  omissions: { from: number; to: number; reason: 'sensitive_identity' | 'not_retained' }[]
  next_sequence: number; watermark: number; finished: boolean; complete: boolean
  completeness_digest: string
}
export type EvidenceDescriptor = {
  evidence_id: string; run_id: string; attempt_id?: string; node_run_id?: string
  source_path?: string; capture_group?: string; kind: string
  availability: 'available' | 'unavailable'; omission_reason?: string
  byte_length?: number; digest?: string; created_at: string; redacted: boolean
}
export type EvidenceDescriptorPage = { descriptors: EvidenceDescriptor[]; next_cursor?: string; generation: number; complete: boolean }
export type EvidenceChunk = {
  availability: 'available' | 'unavailable'; omission_reason?: string
  total_length: number; digest?: string; bytes_base64?: string; next_offset: number; end: boolean
}
export type HistoryPruneResult = {
  removed_runs: number; removed_files: number; skipped_active: number
  skipped_interrupted: number; skipped_pinned: number; skipped_pending: number
}

export type AgentPlaneAPI = {
  chooseProjectFolder(): Promise<ProjectView | null>
  listProjects(limit: number, cursor?: string): Promise<ProjectPage>
  addWorkspace(projectID: string, relativePath: string): Promise<WorkspaceView>
  listWorkspaces(projectID: string, limit: number, cursor?: string): Promise<WorkspacePage>
  getSettings(): Promise<LocalSettings>
  updateSettings(expectedRevision: number, changes: Record<string, unknown>): Promise<LocalSettings>
  detectTool(toolID: 'git' | 'codex'): Promise<ToolStatus>
  openDocument(projectID: string, relativePath: string): Promise<DocumentView>
  updateDocument(documentID: string, expectedRevision: number, text: string): Promise<DocumentView>
  saveDocument(documentID: string, expectedRevision: number, expectedDiskDigest: string, acceptReformat: boolean): Promise<DocumentView>
  validateGraph(documentID: string, revision: number): Promise<DiagnosticSummary>
  preflightRun(selection: RunSelection): Promise<PreflightResult>
  startRun(selection: StartSelection): Promise<StartResult>
  reattachRun(): Promise<StartResult | null>
  cancelRun(runID: string): Promise<RunCancelResult>
  snapshotRun(runID: string): Promise<RunSnapshot>
  eventsRun(runID: string, afterSequence: number, limit: number): Promise<EventPage>
  listHistory(selection: { project_id?: string; graph_id?: string; storage_state?: string; started_from?: string; started_before?: string; cursor?: string; limit: number }): Promise<HistoryListPage>
  getHistory(runID: string): Promise<HistoryDetail>
  outcomesHistory(runID: string, limit: number, nodeCursor?: string, attemptCursor?: string): Promise<HistoryOutcomePage>
  eventsHistory(runID: string, afterSequence: number, limit: number): Promise<HistoryEventPage>
  listEvidence(runID: string, limit: number, attemptID?: string, cursor?: string): Promise<EvidenceDescriptorPage>
  evidenceChunk(evidenceID: string, offset: number, limit: number): Promise<EvidenceChunk>
  pinHistory(runID: string, expectedRevision: number, pinned: boolean): Promise<{ revision: number; pinned: boolean }>
  pruneHistory(retentionDays?: number, keepAll?: boolean): Promise<HistoryPruneResult>
  currentRun(): Promise<CurrentRun>
  onServiceDisconnected(listener: () => void): () => void
  openExternal(url: string): Promise<void>
}

declare global {
  interface Window { agentplane: AgentPlaneAPI }
}
