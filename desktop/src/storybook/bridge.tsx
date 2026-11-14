import type { OpenResponse, ValidateResponse } from '../editor/types'
import type { ApEvent, ApiResponse, GraphSummary, PendingApproval, RecentGraph, RunSummary } from '../types'

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

/** What the graph document API answers in a story (ADR-0021). Responses are recorded from the real API. */
export interface GraphStory {
  /** The graph openGraph() chooses; its recorded open response. */
  opened?: OpenResponse
  /** Answers a validation of typed text. */
  validate?: (text: string) => ValidateResponse
  /** Answers an operation batch with a recorded response; undefined means "the engine could not be reached". */
  edit?: (ops: { op: string }[], text: string) => { status: number; body: unknown } | undefined
  /** How starting a run from the grant ends. Default: ok. */
  run?: 'ok' | 'error'
  /** How a save ends. Default: ok. */
  save?: 'ok' | 'conflict' | 'gone' | 'exists' | 'error'
  /** Makes openGraph() fail with this message. */
  openError?: string
  recent?: RecentGraph[]
}

export interface BridgeData {
  graph?: GraphStory
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
    request: async <T,>(method: 'GET' | 'POST' | 'PUT', path: string, body?: unknown): Promise<ApiResponse<T>> => {
      if (data.hang) return new Promise<ApiResponse<T>>(() => undefined)
      if (data.unreachable) throw new Error('the agentplane sidecar is not running')
      const run = /^\/runs\/([^/]+)$/.exec(path)
      const g = data.graph
      if (method === 'POST' && path === '/graphs/open' && g?.opened) return respond(200, g.opened) as ApiResponse<T>
      if (method === 'POST' && path === '/graphs/validate' && body && typeof (body as { text?: unknown }).text === 'string' && g) {
        const text = (body as { text: string }).text
        return respond(200, g.validate ? g.validate(text) : { status: 'valid', diagnostics: [] }) as ApiResponse<T>
      }
      if (method === 'POST' && path === '/graphs/edit' && g) {
        const b = body as { ops?: { op: string }[]; text?: string }
        const answer = g.edit?.(b.ops ?? [], b.text ?? '')
        if (!answer) throw new Error('the engine could not be reached')
        return respond(answer.status, answer.body) as ApiResponse<T>
      }
      if (method === 'POST' && path === '/runs' && g) {
        if (g.run === 'error') return respond(422, { error: { code: 'invalid_graph', message: 'the graph file is not valid' }, diagnostics: [{ severity: 'error', code: 'E_NODE_CONFIG_INVALID', message: 'shell node "work": config.command must not be empty' }] }) as ApiResponse<T>
        return respond(202, { run_id: 'story-run' }) as ApiResponse<T>
      }
      if (method === 'POST' && path === '/graphs/stat' && g?.opened) return respond(200, { exists: true, hash: g.opened.hash, size: g.opened.text.length }) as ApiResponse<T>
      if (method === 'PUT' && path === '/graphs/save' && g) {
        const how = g.save ?? 'ok'
        if (how === 'ok') return respond(200, { hash: `saved-${Math.abs((body as { text?: string })?.text?.length ?? 0)}` }) as ApiResponse<T>
        if (how === 'conflict') return respond(409, { error: { code: 'conflict', message: 'the file changed on disk since you last saw it' }, current_hash: 'on-disk-hash' }) as ApiResponse<T>
        if (how === 'error') return respond(500, { error: { code: 'write_failed', message: 'the file could not be written; the previous version is unchanged' } }) as ApiResponse<T>
        return respond(409, { error: { code: how, message: how } }) as ApiResponse<T>
      }
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
    openGraph: async () => {
      const g = data.graph
      if (g?.openError) return { ok: false, code: 'not_found', message: g.openError }
      return g?.opened ? { ok: true, grant: 'story-grant', label: g.opened.label, exists: true } : { canceled: true }
    },
    newGraph: async () => (data.graph ? { ok: true, grant: 'story-new', label: 'new-graph.yaml', exists: false } : { canceled: true }),
    recentGraphs: async () => data.graph?.recent ?? [],
    openRecentGraph: async () => (data.graph?.opened ? { ok: true, grant: 'story-grant', label: data.graph.opened.label, exists: true } : { ok: false, code: 'not_found', message: 'that graph is no longer in the list' }),
    touchRecent: async () => true,
    closeGraph: async () => true,
  }
}
