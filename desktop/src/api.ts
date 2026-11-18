import type { ApiResponse, Diagnostic, GraphSummary, PendingApproval, RunSummary } from './types'

// Thin typed wrappers over the preload bridge. Nothing here talks to the
// network: the main process owns the sidecar address and token.
const bridge = () => window.agentplane

/** A list endpoint answered with something other than a list. Callers show an error state, never an empty list. */
export class ApiError extends Error {
  constructor(readonly status: number, path: string) {
    super(`GET ${path} failed (${status})`)
  }
}

async function getList<K extends string, T>(path: string, key: K): Promise<T[]> {
  const r = await bridge().request<Record<K, T[]>>('GET', path)
  const list = r.body?.[key]
  if (r.status !== 200 || !Array.isArray(list)) throw new ApiError(r.status, path)
  return list
}

export const listRuns = (): Promise<RunSummary[]> => getList<'runs', RunSummary>('/runs', 'runs')

/** What the engine says about a run ID: found, unsupported (a newer event schema) or missing. */
export type RunLookup =
  | { kind: 'found'; summary: GraphSummary; graph: string; run: RunSummary }
  | { kind: 'unsupported' }
  | { kind: 'missing' }

// The engine answers a run written by a newer build with a typed 422 whose code
// is stable (internal/api/readfailure.go); the message is English and not shown.
const UNSUPPORTED_SCHEMA = 'unsupported_schema'

export async function lookupRun(id: string): Promise<RunLookup> {
  const r = await bridge().request<{ run: RunSummary; graph: string; graph_summary: GraphSummary } & ErrorBody>('GET', `/runs/${encodeURIComponent(id)}`)
  if (r.status === 422 && r.body?.error?.code === UNSUPPORTED_SCHEMA) return { kind: 'unsupported' }
  if (r.status !== 200 || !r.body || !('run' in r.body)) return { kind: 'missing' }
  return { kind: 'found', summary: r.body.graph_summary, graph: r.body.graph, run: r.body.run }
}

export async function getRun(id: string): Promise<{ summary: GraphSummary; graph: string; run: RunSummary } | null> {
  const found = await lookupRun(id)
  return found.kind === 'found' ? { summary: found.summary, graph: found.graph, run: found.run } : null
}

export const listApprovals = (): Promise<PendingApproval[]> => getList<'approvals', PendingApproval>('/approvals', 'approvals')

export interface ApiFailure {
  message: string
  diagnostics: Diagnostic[]
}
type ErrorBody = { error?: { code?: string; message?: string }; diagnostics?: Diagnostic[] }

function failure(r: ApiResponse<ErrorBody>): ApiFailure {
  return { message: r.body?.error?.message ?? `request failed (${r.status})`, diagnostics: r.body?.diagnostics ?? [] }
}

/** Validates the granted file as it is on disk. */
export async function validateGraph(grant: string): Promise<{ status: string; diagnostics: Diagnostic[] } | ApiFailure> {
  const r = await bridge().request<{ status: string; diagnostics: Diagnostic[] } & ErrorBody>('POST', '/graphs/validate', { grant })
  if (r.status === 200 && r.body) return { status: r.body.status, diagnostics: r.body.diagnostics }
  return failure(r as ApiResponse<ErrorBody>)
}

/** Starts a run from the saved bytes of the granted file. */
export async function startRun(grant: string, inputs: string[]): Promise<{ runId: string } | ApiFailure> {
  const r = await bridge().request<{ run_id: string } & ErrorBody>('POST', '/runs', { graph_grant: grant, inputs })
  if (r.status === 202 && r.body && 'run_id' in r.body) return { runId: r.body.run_id }
  return failure(r as ApiResponse<ErrorBody>)
}

export async function decide(runId: string, nodeRunId: string, approve: boolean, comment: string): Promise<ApiFailure | null> {
  const r = await bridge().request<ErrorBody>('POST', `/runs/${encodeURIComponent(runId)}/decisions/${encodeURIComponent(nodeRunId)}`, {
    decision: approve ? 'approved' : 'rejected',
    comment,
  })
  // 409 means somebody decided first: not an error for the person clicking.
  return r.status === 200 || r.status === 409 ? null : failure(r)
}
