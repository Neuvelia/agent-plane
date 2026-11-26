// Wire shapes of the AgentPlane event stream (event schema 11; schemas 1-10 are
// still delivered for older stored runs). Payloads are kept loose on purpose:
// the reducer reads only the fields it needs and ignores the rest, so additive
// schema changes never break the renderer. Schema 10 adds opt-in
// value_recorded events (ADR-0023) and schema 11 workspace isolation facts
// (ADR-0024); the run views display neither.
export interface ApEvent {
  event_schema_version: number
  type: string
  timestamp: string
  sequence: number
  run_id: string
  graph_id: string
  graph_version: string
  node_id?: string
  node_run_id?: string
  attempt_id?: string
  payload: Record<string, unknown>
}

export interface GraphSummary {
  nodes: { id: string; type: string; max_attempts?: number }[]
  edges: { from: string; to: string }[]
}

export interface RunSummary {
  run_id: string
  graph_id: string
  started_at: string
  status: string
  event_count: number
  /** Semantic version of the executed graph; equals the document API's `version` for the same bytes. Absent in older stores. */
  graph_version?: string
  /** Timestamp of run_finished; absent while the run has none. */
  finished_at?: string
  /** Repository identity recorded in run_started (F2.9); absent for older runs or outside a Git work tree. Branch is absent on a detached HEAD. */
  repo_name?: string
  repo_branch?: string
  repo_commit?: string
  /** Engine version that produced the run. */
  runtime_version?: string
  truncated?: boolean
  /** Last durable sequence of a run that has no terminal event and no live writer. */
  interrupted_at_sequence?: number
  /** Reason a run is listed as corrupt or unsupported. */
  error?: string
}

export interface PendingApproval {
  run_id: string
  node_id: string
  node_run_id: string
  prompt: string
  context: { name: string; value: string; truncated?: boolean }[]
}

export interface Diagnostic {
  severity: string
  code: string
  message: string
  suggestion?: string
  location?: { node_id?: string; edge_index?: number; graph_input_binding_index?: number; path?: string }
  /** 1-based source line, when the engine could resolve it (document API only). */
  line?: number
}

export interface ApiResponse<T = unknown> {
  status: number
  body: T | null
}

/** What the main process returns for a graph the person chose (ADR-0021). */
export type GraphGrantResult =
  | { ok: true; grant: string; label: string; exists: boolean }
  | { ok: false; code: string; message: string }
  | { canceled: true }

/** A graph file the person chose: the grant addresses it, the label is its file name. */
export interface ChosenGraph {
  grant: string
  label: string
}

export interface RecentGraph {
  id: string
  label: string
}

/**
 * "Record run values" (ADR-0023). `enabled` and `redactEnv` are the saved
 * preference; `active` and `missing` describe the running engine, which keeps
 * its flags until the app restarts. Environment values are never exposed.
 */
export interface RecordingSettings {
  enabled: boolean
  redactEnv: string[]
  active: boolean
  missing: string[]
}

export type RecordingSaveResult = { ok: true; settings: RecordingSettings } | { ok: false; code: 'invalid' | 'save_failed'; name?: string }

declare global {
  interface Window {
    agentplane: {
      /** Desktop app version (Electron app.getVersion()); read-only, display only (F2.11). */
      readonly appVersion: string
      request: <T = unknown>(method: 'GET' | 'POST' | 'PUT', path: string, body?: unknown) => Promise<ApiResponse<T>>
      subscribe: (runId: string, onEvent: (e: ApEvent) => void, onEnd: (reason: string) => void) => () => void
      openGraph: () => Promise<GraphGrantResult>
      newGraph: () => Promise<GraphGrantResult>
      recentGraphs: () => Promise<RecentGraph[]>
      openRecentGraph: (id: string) => Promise<GraphGrantResult>
      touchRecent: (grant: string) => Promise<boolean>
      closeGraph: (grant: string) => Promise<boolean>
      recordingSettings: () => Promise<RecordingSettings>
      setRecordingSettings: (settings: { enabled: boolean; redactEnv: string[] }) => Promise<RecordingSaveResult>
    }
  }
}
