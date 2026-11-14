import type { ApEvent, ApiResponse, GraphSummary, PendingApproval, RunSummary } from '../types'

// In-memory stand-in for the Electron preload bridge (window.agentplane).
// It replays recorded fixtures: no sidecar, no network, no filesystem.

export type StreamEnding = 'open' | 'end' | 'lost'

export interface BridgeRun {
  summary: RunSummary
  graph: GraphSummary
  yaml: string
  events: ApEvent[]
  /** open: stream stays live; end: server's normal end; lost: connection drops. */
  ending: StreamEnding
}

export interface BridgeData {
  runs: BridgeRun[]
  approvals?: PendingApproval[]
  validate?: { status: string; diagnostics: unknown[] }
  /** Path returned by the "Choose…" dialog. */
  pickedGraph?: string | null
  /** Every request rejects, as when the sidecar is not running. */
  unreachable?: boolean
  /** Every request stays pending forever: the loading state. */
  hang?: boolean
  /** Makes every approval decision fail with this HTTP status and message. */
  decisionFailure?: { status: number; message: string }
}

export function createBridge(data: BridgeData): Window['agentplane'] {
  const respond = <T,>(status: number, body: T): ApiResponse<T> => ({ status, body })
  return {
    request: async <T,>(method: 'GET' | 'POST', path: string): Promise<ApiResponse<T>> => {
      if (data.hang) return new Promise<ApiResponse<T>>(() => undefined)
      if (data.unreachable) throw new Error('the agentplane sidecar is not running')
      const run = /^\/runs\/([^/]+)$/.exec(path)
      if (method === 'GET' && path === '/runs') return respond(200, { runs: data.runs.map((r) => r.summary) }) as ApiResponse<T>
      if (method === 'GET' && run) {
        const r = data.runs.find((x) => x.summary.run_id === decodeURIComponent(run[1]!))
        return (r ? respond(200, { run: r.summary, graph: r.yaml, graph_summary: r.graph }) : respond(404, null)) as ApiResponse<T>
      }
      if (method === 'GET' && path === '/approvals') return respond(200, { approvals: data.approvals ?? [] }) as ApiResponse<T>
      if (method === 'POST' && path === '/graphs/validate') return respond(200, data.validate ?? { status: 'valid', diagnostics: [] }) as ApiResponse<T>
      if (method === 'POST' && path === '/runs') return respond(202, { run_id: data.runs[0]?.summary.run_id ?? 'story' }) as ApiResponse<T>
      if (method === 'POST' && path.startsWith('/runs/') && data.decisionFailure) return respond(data.decisionFailure.status, { error: { message: data.decisionFailure.message } }) as ApiResponse<T>
      if (method === 'POST' && path.startsWith('/runs/')) return respond(200, {}) as ApiResponse<T>
      return respond(404, null) as ApiResponse<T>
    },
    subscribe: (runId, onEvent, onEnd) => {
      let alive = true
      const r = data.runs.find((x) => x.summary.run_id === runId)
      queueMicrotask(() => {
        if (!alive || !r) return
        for (const e of r.events) onEvent(e)
        if (r.ending === 'end') onEnd('end')
        else if (r.ending === 'lost') onEnd('eof')
      })
      return () => {
        alive = false
      }
    },
    pickGraph: async () => data.pickedGraph ?? null,
  }
}
