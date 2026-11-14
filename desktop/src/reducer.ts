import type { ApEvent } from './types'

// All run state shown by the UI is a pure function of the event stream. No
// component keeps node status of its own, and nothing here uses timers: a
// stored run and a live run reduce to the same state (F2.5).

export type NodeStatus =
  | 'pending'
  | 'ready'
  | 'running'
  | 'retrying'
  | 'waiting_human'
  | 'succeeded'
  | 'failed'
  | 'skipped'
  | 'cancelled'

export type RunStatus = 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'

export interface Evidence {
  kind: string
  source: string
  path?: string
  exitCode?: number
  stdout?: string
  stderr?: string
  omitted?: number
}

export interface CheckView {
  checkId: string
  kind: string
  verdict: string
  reason: string
  evidence: Evidence[]
  durationMs: number
}

export interface Attempt {
  id: string
  number: number
  max: number
  outcome?: string
  check?: CheckView
  log: string
}

export interface ApprovalView {
  prompt: string
  context: { name: string; value: string; truncated?: boolean }[]
  decision?: string
  comment?: string
}

export interface NodeView {
  id: string
  nodeRunId?: string
  status: NodeStatus
  attempts: Attempt[]
  log: string
  approval?: ApprovalView
  failureKind?: string
  diagnosticCodes: string[]
  durationMs?: number
  startedSeq?: number
  finishedSeq?: number
}

export interface RunView {
  runId?: string
  graphId?: string
  status: RunStatus
  nodeCount: number
  nodes: Record<string, NodeView>
  order: string[]
  lastSequence: number
  eventCount: number
  startedAt?: string
  finishedAt?: string
  failureKind?: string
}

export const MAX_LOG_CHARS = 64 * 1024

export function initialRun(): RunView {
  return { status: 'running', nodeCount: 0, nodes: {}, order: [], lastSequence: 0, eventCount: 0 }
}

function node(state: RunView, id: string): NodeView {
  let n = state.nodes[id]
  if (!n) {
    n = { id, status: 'pending', attempts: [], log: '', diagnosticCodes: [] }
    state.nodes[id] = n
    state.order.push(id)
  }
  return n
}

function decode(base64: unknown): string {
  if (typeof base64 !== 'string' || base64 === '') return ''
  const binary = atob(base64)
  const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

function bounded(text: string): string {
  return text.length <= MAX_LOG_CHARS ? text : text.slice(text.length - MAX_LOG_CHARS)
}

const num = (v: unknown, fallback = 0): number => (typeof v === 'number' ? v : fallback)
const str = (v: unknown): string => (typeof v === 'string' ? v : '')

function attemptFor(n: NodeView, attemptId: string | undefined): Attempt | undefined {
  if (attemptId) {
    const found = n.attempts.find((a) => a.id === attemptId)
    if (found) return found
  }
  return n.attempts[n.attempts.length - 1]
}

// reduce returns a new state; it never mutates its input. An event whose
// sequence is not newer than the last applied one is ignored, which makes
// replay-then-follow (and reconnects) idempotent.
export function reduce(previous: RunView, event: ApEvent): RunView {
  if (event.sequence <= previous.lastSequence) return previous
  const state: RunView = structuredClone(previous)
  state.lastSequence = event.sequence
  state.eventCount += 1
  state.runId = event.run_id
  state.graphId = event.graph_id
  const p = event.payload
  const id = event.node_id

  switch (event.type) {
    case 'run_started':
      state.nodeCount = num(p.node_count)
      state.startedAt = event.timestamp
      break
    case 'run_finished':
      state.status = (str(p.status) as RunStatus) || 'failed'
      state.finishedAt = event.timestamp
      if (p.failure_kind) state.failureKind = str(p.failure_kind)
      break
    case 'node_ready':
      if (id) node(state, id).status = 'ready'
      break
    case 'node_started':
      if (id) {
        const n = node(state, id)
        n.status = 'running'
        n.nodeRunId = event.node_run_id
        n.startedSeq = event.sequence
      }
      break
    case 'node_finished':
    case 'node_terminated':
      if (id) {
        const n = node(state, id)
        n.status = (str(p.status) as NodeStatus) || 'failed'
        n.nodeRunId = event.node_run_id
        n.finishedSeq = event.sequence
        n.failureKind = p.failure_kind ? str(p.failure_kind) : undefined
        n.diagnosticCodes = Array.isArray(p.diagnostic_codes) ? (p.diagnostic_codes as string[]) : []
        if (typeof p.duration_ms === 'number') n.durationMs = p.duration_ms
      }
      break
    case 'attempt_started':
      if (id && event.attempt_id) {
        const n = node(state, id)
        n.attempts.push({ id: event.attempt_id, number: num(p.number), max: num(p.max_attempts), log: '' })
      }
      break
    case 'attempt_finished':
      if (id) {
        const a = attemptFor(node(state, id), event.attempt_id)
        if (a) a.outcome = str(p.outcome)
      }
      break
    case 'gate_check':
      if (id) {
        const n = node(state, id)
        const a = attemptFor(n, event.attempt_id)
        const evidence = (Array.isArray(p.evidence) ? p.evidence : []) as Record<string, unknown>[]
        const view: CheckView = {
          checkId: str(p.check_id),
          kind: str(p.kind),
          verdict: str(p.verdict),
          reason: str(p.reason),
          durationMs: Math.round(num(p.duration_ns) / 1e6),
          evidence: evidence.map((e) => ({
            kind: str(e.kind),
            source: str(e.source),
            path: e.path ? str(e.path) : undefined,
            exitCode: typeof e.exit_code === 'number' ? e.exit_code : undefined,
            stdout: e.stdout ? decode(e.stdout) : undefined,
            stderr: e.stderr ? decode(e.stderr) : undefined,
            omitted: typeof e.omitted === 'number' ? e.omitted : undefined,
          })),
        }
        if (a) a.check = view
      }
      break
    case 'log_chunk':
      if (id) {
        const n = node(state, id)
        const text = decode(p.data)
        n.log = bounded(n.log + text)
        const a = attemptFor(n, event.attempt_id)
        if (a && event.attempt_id) a.log = bounded(a.log + text)
      }
      break
    case 'approval_requested':
      if (id) {
        const n = node(state, id)
        n.nodeRunId = event.node_run_id
        n.status = 'waiting_human'
        n.approval = {
          prompt: str(p.prompt),
          context: (Array.isArray(p.context) ? p.context : []) as ApprovalView['context'],
        }
      }
      break
    case 'approval_decided':
      if (id) {
        const n = node(state, id)
        n.approval = { ...(n.approval ?? { prompt: '', context: [] }), decision: str(p.decision), comment: str(p.comment) || undefined }
        // The node is no longer waiting; node_finished carries its terminal status.
        if (n.status === 'waiting_human') n.status = 'running'
      }
      break
    default:
      break
  }
  return state
}

export function reduceAll(events: ApEvent[], from: RunView = initialRun()): RunView {
  return events.reduce(reduce, from)
}

// nodeView is how the UI reads a node: a node that no event has mentioned yet
// is pending by definition. The graph (not the stream) says which nodes exist.
export function nodeView(run: RunView, id: string): NodeView {
  return run.nodes[id] ?? { id, status: 'pending', attempts: [], log: '', diagnosticCodes: [] }
}

// displayStatus turns a retried, still-running node into "retrying" without
// storing anything: it is derived from attempts that already exist.
export function displayStatus(n: NodeView): NodeStatus {
  if (n.status === 'running') {
    const last = n.attempts[n.attempts.length - 1]
    if (last && last.number > 1 && last.outcome === undefined) return 'retrying'
  }
  return n.status
}

// classifyEnd interprets why an event stream ended. Only the server's explicit
// "end" means the run's stored events are complete; anything else is a lost
// connection, and the UI must say so instead of inventing a run status.
export type StreamEnd = { kind: 'complete' } | { kind: 'lost'; message: string }

export function classifyEnd(reason: string): StreamEnd {
  if (reason === 'end') return { kind: 'complete' }
  if (reason === 'server_error') return { kind: 'lost', message: 'The engine stopped this run because it could not be stored.' }
  return { kind: 'lost', message: 'The connection to the engine ended unexpectedly.' }
}

// A run that has no terminal event and is not being followed is interrupted.
export function settle(run: RunView, live: boolean): RunView {
  if (run.status === 'running' && !live && run.eventCount > 0) return { ...run, status: 'interrupted' }
  return run
}

// presentRun is what the inspector shows for a stream in a given end state:
// a normally ended log without a terminal event is "interrupted"; a stream that
// is still followed, or whose connection was lost, keeps the reduced status.
export function presentRun(run: RunView, endedNormally: boolean, lost: boolean): RunView {
  return settle(run, !endedNormally || lost)
}

export function pendingApprovals(run: RunView): NodeView[] {
  return run.order.map((id) => run.nodes[id]!).filter((n) => n.status === 'waiting_human')
}
