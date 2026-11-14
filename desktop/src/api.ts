import type { ApiResponse, Diagnostic, GraphSummary, PendingApproval, RunSummary } from './types'

// Thin typed wrappers over the preload bridge. Nothing here talks to the
// network: the main process owns the sidecar address and token.
const bridge = () => window.agentplane

export async function listRuns(): Promise<RunSummary[]> {
  const r = await bridge().request<{ runs: RunSummary[] }>('GET', '/runs')
  return r.body?.runs ?? []
}

export async function getRun(id: string): Promise<{ summary: GraphSummary; graph: string; run: RunSummary } | null> {
  const r = await bridge().request<{ run: RunSummary; graph: string; graph_summary: GraphSummary }>('GET', `/runs/${encodeURIComponent(id)}`)
  if (r.status !== 200 || !r.body) return null
  return { summary: r.body.graph_summary, graph: r.body.graph, run: r.body.run }
}

export async function listApprovals(): Promise<PendingApproval[]> {
  const r = await bridge().request<{ approvals: PendingApproval[] }>('GET', '/approvals')
  return r.body?.approvals ?? []
}

export interface ApiFailure {
  message: string
  diagnostics: Diagnostic[]
}
type ErrorBody = { error?: { message?: string }; diagnostics?: Diagnostic[] }

function failure(r: ApiResponse<ErrorBody>): ApiFailure {
  return { message: r.body?.error?.message ?? `request failed (${r.status})`, diagnostics: r.body?.diagnostics ?? [] }
}

export async function validateGraph(path: string): Promise<{ status: string; diagnostics: Diagnostic[] } | ApiFailure> {
  const r = await bridge().request<{ status: string; diagnostics: Diagnostic[] } & ErrorBody>('POST', '/graphs/validate', { path })
  if (r.status === 200 && r.body) return { status: r.body.status, diagnostics: r.body.diagnostics }
  return failure(r as ApiResponse<ErrorBody>)
}

export async function startRun(graphPath: string, inputs: string[]): Promise<{ runId: string } | ApiFailure> {
  const r = await bridge().request<{ run_id: string } & ErrorBody>('POST', '/runs', { graph_path: graphPath, inputs })
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
