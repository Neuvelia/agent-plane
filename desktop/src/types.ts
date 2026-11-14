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
  location?: { node_id?: string; path?: string }
}

export interface ApiResponse<T = unknown> {
  status: number
  body: T | null
}

declare global {
  interface Window {
    agentplane: {
      request: <T = unknown>(method: 'GET' | 'POST', path: string, body?: unknown) => Promise<ApiResponse<T>>
      subscribe: (runId: string, onEvent: (e: ApEvent) => void, onEnd: (reason: string) => void) => () => void
      pickGraph: () => Promise<string | null>
    }
  }
}
