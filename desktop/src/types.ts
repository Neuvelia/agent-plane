// Wire shapes of the AgentPlane event stream (event schema 9). Payloads are
// kept loose on purpose: the reducer reads only the fields it needs and ignores
// the rest, so additive schema changes never break the renderer.
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
  truncated?: boolean
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

export interface RecentGraph {
  id: string
  label: string
}

declare global {
  interface Window {
    agentplane: {
      request: <T = unknown>(method: 'GET' | 'POST' | 'PUT', path: string, body?: unknown) => Promise<ApiResponse<T>>
      subscribe: (runId: string, onEvent: (e: ApEvent) => void, onEnd: (reason: string) => void) => () => void
      pickGraph: () => Promise<string | null>
      openGraph: () => Promise<GraphGrantResult>
      newGraph: () => Promise<GraphGrantResult>
      recentGraphs: () => Promise<RecentGraph[]>
      openRecentGraph: (id: string) => Promise<GraphGrantResult>
      touchRecent: (grant: string) => Promise<boolean>
      closeGraph: (grant: string) => Promise<boolean>
    }
  }
}
