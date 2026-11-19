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
  /** Derived for a node that started and never finished in a run that was interrupted (F3.2). Never in an event. */
  | 'interrupted'

export type RunStatus = 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'

export interface Evidence {
  kind: string
  source: string
  path?: string
  exitCode?: number
  stdout?: string
  stderr?: string
  omitted?: number
  /** Typed reference to evidence held elsewhere, e.g. a diff that is `unavailable`. */
  reference?: EvidenceReference
}

export interface EvidenceReference {
  kind: string
  state: string
  reason?: string
}

/** `changed_files_scope` details of a check, as the engine recorded them (F2.7 FR3). */
export interface CheckScopeView {
  mode: string
  /** Absent when the engine redacted them (`redacted`). */
  allowedPaths?: string[]
  redacted: boolean
  observedCount?: number
}

export interface CheckView {
  checkId: string
  kind: string
  verdict: string
  reason: string
  evidence: Evidence[]
  durationMs: number
  /** `scope.checked_inputs`: machine names of the inputs the check looked at. */
  checkedInputs: string[]
  scope?: CheckScopeView
}

/** From `attempt_input` (F2.7 FR15): what the attempt was given, never the task itself. */
export interface AttemptInput {
  /** The task fingerprint, a hash: it says whether two attempts had the same task, nothing about content. */
  fingerprint: string
  /** Undefined when the engine omitted the scope entries. */
  scope?: string[]
  scopeOmitted: boolean
  contextRefsPresent: boolean
  environmentRefsPresent: boolean
}

/** One observable item of agent progress (F2.7 FR8). Text is normalized and redacted by the engine. */
export type ActivityItem =
  | { kind: 'text'; sourceId?: string; text: string }
  | { kind: 'tool'; callId: string; name: string; status: string }
  | { kind: 'command'; commandId: string; status: string; exitCode?: number }
  | { kind: 'file'; path: string; action: string }
  | { kind: 'status'; code: string }
  | { kind: 'omitted'; retainedEvents: number; retainedBytes: number; liveEvents: number; liveBytes: number; liveStopped: boolean }

/** Bounded progress of one attempt, or of a node with no attempt IDs. */
export interface Activity {
  items: ActivityItem[]
  /** Items dropped, oldest first, to stay within MAX_ACTIVITY_ITEMS and MAX_ACTIVITY_TEXT. */
  dropped: number
}

export const MAX_ACTIVITY_ITEMS = 200
export const MAX_ACTIVITY_TEXT = 64 * 1024

export interface Attempt {
  id: string
  number: number
  max: number
  outcome?: string
  check?: CheckView
  log: string
  /** Event timestamps (ISO) of attempt_started / attempt_finished. */
  startedAt?: string
  finishedAt?: string
  /**
   * Sorted Git-observed paths from this attempt's node_evidence. Undefined means
   * not observed (no evidence event), which is different from an empty list.
   */
  changedFiles?: string[]
  /** Feedback linkage from attempt_input: the attempt and check whose result this attempt received. */
  previousAttemptId?: string
  previousCheckId?: string
  input?: AttemptInput
  activity: Activity
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
  /** The check of a standalone Gate node: its gate_check carries no attempt ID, so it has no Attempt. */
  check?: CheckView
  log: string
  approval?: ApprovalView
  failureKind?: string
  diagnosticCodes: string[]
  durationMs?: number
  startedSeq?: number
  finishedSeq?: number
  /** Event timestamps (ISO) of node_started / node_finished, for the timeline. */
  startedAt?: string
  finishedAt?: string
  /** Adapter reported by node_evidence (F2.7 FR1). */
  adapter?: { id: string; version: string }
  /**
   * Sorted Git-observed paths from a node_evidence event without an attempt ID (a node with no loop).
   * Undefined means not observed. Never merged with an attempt's list.
   */
  changedFiles?: string[]
  /** Progress events that carry no attempt ID. */
  activity: Activity
  /** Characters of the node log dropped from its head by MAX_LOG_CHARS (F2.7 FR16). */
  logDropped: number
  /** Event timestamp (ISO) of node_ready: when the scheduler could first start the node (F2.6 FR1). */
  readyAt?: string
  /** Event timestamps (ISO) of approval_requested / approval_decided of a human node. */
  approvalRequestedAt?: string
  approvalDecidedAt?: string
}

/** Which EventLog filter an event belongs to. Decided here, once, from the event itself. */
export type EventKind = 'failed' | 'retried' | 'human'

export interface EventSummary {
  sequence: number
  timestamp: string
  type: string
  node?: string
  detail?: string
  kind?: EventKind
}

/** The EventLog never needs more than this many rows, so the view keeps no more. */
export const MAX_EVENT_ROWS = 200

export interface RunView {
  runId?: string
  graphId?: string
  /** Event schema of the stream, from its first event: below 3 cannot carry progress (F2.7 FR9). */
  schemaVersion?: number
  /** Semantic version of the graph the run executed, from the events' graph_version (F2.6 FR1). */
  graphVersion?: string
  status: RunStatus
  nodeCount: number
  nodes: Record<string, NodeView>
  order: string[]
  lastSequence: number
  eventCount: number
  startedAt?: string
  finishedAt?: string
  /** Timestamp of the newest applied event: "now" for a run that is still going. */
  lastEventAt?: string
  /** The newest MAX_EVENT_ROWS events, oldest first. log_chunk (Log view) and value_recorded (edge values) are excluded. */
  tail: EventSummary[]
  failureKind?: string
}

export const MAX_LOG_CHARS = 64 * 1024

export function initialRun(): RunView {
  return { status: 'running', nodeCount: 0, nodes: {}, order: [], lastSequence: 0, eventCount: 0, tail: [] }
}

export const emptyActivity = (): Activity => ({ items: [], dropped: 0 })

function node(state: RunView, id: string): NodeView {
  let n = state.nodes[id]
  if (!n) {
    n = { id, status: 'pending', attempts: [], log: '', diagnosticCodes: [], activity: emptyActivity(), logDropped: 0 }
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

function reference(v: unknown): EvidenceReference | undefined {
  if (!v || typeof v !== 'object') return undefined
  const r = v as Record<string, unknown>
  return { kind: str(r.kind), state: str(r.state), reason: r.reason ? str(r.reason) : undefined }
}

function attemptFor(n: NodeView, attemptId: string | undefined): Attempt | undefined {
  if (attemptId) {
    const found = n.attempts.find((a) => a.id === attemptId)
    if (found) return found
  }
  return n.attempts[n.attempts.length - 1]
}

const strings = (v: unknown): string[] | undefined => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : undefined)

function scopeDetail(v: unknown): CheckScopeView | undefined {
  if (!v || typeof v !== 'object') return undefined
  const s = v as Record<string, unknown>
  const redacted = s.scope_redacted === true
  return {
    mode: str(s.mode),
    allowedPaths: redacted ? undefined : strings(s.allowed_paths),
    redacted,
    observedCount: typeof s.observed_count === 'number' ? s.observed_count : undefined,
  }
}

/** Progress of an attempt when the event names one that exists, else of the node. */
function activityOf(n: NodeView, attemptId: string | undefined): Activity {
  return (attemptId ? n.attempts.find((a) => a.id === attemptId)?.activity : undefined) ?? n.activity
}

function mergeById(items: ActivityItem[], match: (i: ActivityItem) => boolean, next: ActivityItem): void {
  const at = items.findIndex(match)
  if (at >= 0) items[at] = next
  else items.push(next)
}

// addActivity merges one normalized event into the bounded activity (F2.7 FR8): text by source ID in
// arrival order, a tool call or command by its ID into one item with its latest status. A finish without
// a start becomes its own item. Oldest items are dropped first; the counter keeps how many.
function addActivity(a: Activity, event: ApEvent): void {
  const p = event.payload
  const items = a.items
  switch (event.type) {
    case 'text_delta': {
      const sourceId = str(p.source_id) || undefined
      const last = items[items.length - 1]
      const target = sourceId ? items.find((i) => i.kind === 'text' && i.sourceId === sourceId) : last?.kind === 'text' && !last.sourceId ? last : undefined
      if (target?.kind === 'text') target.text += str(p.text)
      else items.push({ kind: 'text', sourceId, text: str(p.text) })
      break
    }
    case 'tool_call_started':
    case 'tool_call_finished': {
      const callId = str(p.call_id)
      mergeById(items, (i) => i.kind === 'tool' && i.callId === callId, { kind: 'tool', callId, name: str(p.name), status: str(p.status) })
      break
    }
    case 'command_started':
    case 'command_finished': {
      const commandId = str(p.command_id)
      const exitCode = typeof p.exit_code === 'number' ? p.exit_code : undefined
      mergeById(items, (i) => i.kind === 'command' && i.commandId === commandId, { kind: 'command', commandId, status: str(p.status), exitCode })
      break
    }
    case 'file_changed':
      items.push({ kind: 'file', path: str(p.path), action: str(p.action) })
      break
    case 'status':
      items.push({ kind: 'status', code: str(p.code) })
      break
    default:
      items.push({ kind: 'omitted', retainedEvents: num(p.retained_events), retainedBytes: num(p.retained_bytes), liveEvents: num(p.live_events), liveBytes: num(p.live_bytes), liveStopped: p.live_stopped === true })
  }
  bound(a)
}

function bound(a: Activity): void {
  const textSize = () => a.items.reduce((n, i) => n + (i.kind === 'text' ? i.text.length : 0), 0)
  while (a.items.length > MAX_ACTIVITY_ITEMS || (textSize() > MAX_ACTIVITY_TEXT && a.items.length > 1)) {
    a.items.shift()
    a.dropped += 1
  }
  const only = a.items[0]
  if (a.items.length === 1 && only?.kind === 'text' && only.text.length > MAX_ACTIVITY_TEXT) only.text = only.text.slice(only.text.length - MAX_ACTIVITY_TEXT)
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
  state.graphVersion = event.graph_version || state.graphVersion
  state.schemaVersion ??= event.event_schema_version
  state.lastEventAt = event.timestamp
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
      if (id) {
        const n = node(state, id)
        n.status = 'ready'
        n.readyAt = event.timestamp
      }
      break
    case 'node_started':
      if (id) {
        const n = node(state, id)
        n.status = 'running'
        n.nodeRunId = event.node_run_id
        n.startedSeq = event.sequence
        n.startedAt = event.timestamp
      }
      break
    case 'node_finished':
    case 'node_terminated':
      if (id) {
        const n = node(state, id)
        n.status = (str(p.status) as NodeStatus) || 'failed'
        n.nodeRunId = event.node_run_id
        n.finishedSeq = event.sequence
        n.finishedAt = event.timestamp
        n.failureKind = p.failure_kind ? str(p.failure_kind) : undefined
        n.diagnosticCodes = Array.isArray(p.diagnostic_codes) ? (p.diagnostic_codes as string[]) : []
        if (typeof p.duration_ms === 'number') n.durationMs = p.duration_ms
      }
      break
    case 'attempt_started':
      if (id && event.attempt_id) {
        const n = node(state, id)
        n.attempts.push({ id: event.attempt_id, number: num(p.number), max: num(p.max_attempts), log: '', startedAt: event.timestamp, activity: emptyActivity() })
      }
      break
    case 'attempt_finished':
      if (id) {
        const a = attemptFor(node(state, id), event.attempt_id)
        if (a) {
          a.outcome = str(p.outcome)
          a.finishedAt = event.timestamp
        }
      }
      break
    case 'attempt_input':
      // Only an exact attempt match: feedback must never be attached to the wrong attempt.
      if (id && event.attempt_id) {
        const a = node(state, id).attempts.find((x) => x.id === event.attempt_id)
        if (a) {
          a.previousAttemptId = str(p.previous_attempt_id) || undefined
          a.previousCheckId = str(p.previous_check_id) || undefined
          a.input = {
            fingerprint: str(p.task_fingerprint),
            scope: p.scope_omitted === true ? undefined : strings(p.intended_scope),
            scopeOmitted: p.scope_omitted === true,
            contextRefsPresent: p.context_refs_present === true,
            environmentRefsPresent: p.environment_refs_present === true,
          }
        }
      }
      break
    case 'node_evidence':
      // With an attempt ID the files belong to that attempt; without one, to the node (no loop, F2.7 FR5).
      // The two are never merged.
      if (id) {
        const n = node(state, id)
        const files = (Array.isArray(p.changed_files) ? p.changed_files : []) as Record<string, unknown>[]
        const paths = [...new Set(files.map((f) => str(f.path)).filter((path) => path !== ''))].sort()
        if (p.adapter_id) n.adapter = { id: str(p.adapter_id), version: str(p.adapter_version) }
        if (!event.attempt_id) n.changedFiles = paths
        else {
          const a = n.attempts.find((x) => x.id === event.attempt_id)
          if (a) a.changedFiles = paths
        }
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
          checkedInputs: strings((p.scope as Record<string, unknown> | undefined)?.checked_inputs) ?? [],
          scope: scopeDetail(p.changed_files_scope),
          evidence: evidence.map((e) => ({
            kind: str(e.kind),
            source: str(e.source),
            path: e.path ? str(e.path) : undefined,
            exitCode: typeof e.exit_code === 'number' ? e.exit_code : undefined,
            stdout: e.stdout ? decode(e.stdout) : undefined,
            stderr: e.stderr ? decode(e.stderr) : undefined,
            omitted: typeof e.omitted === 'number' ? e.omitted : undefined,
            reference: reference(e.reference),
          })),
        }
        if (a) a.check = view
        // A standalone Gate (no loop) emits its check without an attempt ID.
        else if (!event.attempt_id) n.check = view
      }
      break
    case 'log_chunk':
      if (id) {
        const n = node(state, id)
        const text = decode(p.data)
        const joined = n.log + text
        n.log = bounded(joined)
        n.logDropped += joined.length - n.log.length
        const a = attemptFor(n, event.attempt_id)
        if (a && event.attempt_id) a.log = bounded(a.log + text)
      }
      break
    case 'approval_requested':
      if (id) {
        const n = node(state, id)
        n.nodeRunId = event.node_run_id
        n.status = 'waiting_human'
        n.approvalRequestedAt = event.timestamp
        n.approval = {
          prompt: str(p.prompt),
          context: (Array.isArray(p.context) ? p.context : []) as ApprovalView['context'],
        }
      }
      break
    case 'approval_decided':
      if (id) {
        const n = node(state, id)
        n.approvalDecidedAt = event.timestamp
        n.approval = { ...(n.approval ?? { prompt: '', context: [] }), decision: str(p.decision), comment: str(p.comment) || undefined }
        // The node is no longer waiting; node_finished carries its terminal status.
        if (n.status === 'waiting_human') n.status = 'running'
      }
      break
    case 'text_delta':
    case 'tool_call_started':
    case 'tool_call_finished':
    case 'command_started':
    case 'command_finished':
    case 'file_changed':
    case 'status':
    case 'progress_omitted':
      if (id) addActivity(activityOf(node(state, id), event.attempt_id), event)
      break
    default:
      break
  }
  const row = summarize(event)
  if (row) {
    state.tail.push(row)
    if (state.tail.length > MAX_EVENT_ROWS) state.tail.shift()
  }
  return state
}

// summarize is the one place that decides what an event looks like in the EventLog.
// Unknown event types are kept (by type) and simply have no detail or filter kind.
// value_recorded (schema 10, ADR-0023) carries user data for the edge inspector
// (F2.3 E5b); like log_chunk it has its own view and never becomes an EventLog row.
// Normalized progress (ADR-0011) has its own Activity view; progress_omitted stays an EventLog row.
const NORMALIZED = new Set(['text_delta', 'tool_call_started', 'tool_call_finished', 'command_started', 'command_finished', 'file_changed', 'status'])

function summarize(event: ApEvent): EventSummary | null {
  if (event.type === 'log_chunk' || event.type === 'value_recorded' || NORMALIZED.has(event.type)) return null
  const p = event.payload
  const row: EventSummary = { sequence: event.sequence, timestamp: event.timestamp, type: event.type, node: event.node_id }
  switch (event.type) {
    case 'gate_check':
      row.detail = str(p.verdict) || undefined
      if (p.verdict === 'red') row.kind = 'failed'
      break
    case 'attempt_started':
      row.detail = `attempt ${num(p.number)}/${num(p.max_attempts)}`
      if (num(p.number) > 1) row.kind = 'retried'
      break
    case 'attempt_finished':
      row.detail = str(p.outcome) || undefined
      if (p.outcome === 'rejected') row.kind = 'retried'
      break
    case 'node_finished':
    case 'node_terminated':
    case 'run_finished':
      row.detail = str(p.status) || undefined
      if (p.status === 'failed') row.kind = 'failed'
      break
    case 'approval_requested':
      row.kind = 'human'
      break
    case 'approval_decided':
      row.detail = str(p.decision) || undefined
      row.kind = 'human'
      break
    default:
      break
  }
  return row
}

export function reduceAll(events: ApEvent[], from: RunView = initialRun()): RunView {
  return events.reduce(reduce, from)
}

// nodeView is how the UI reads a node: a node that no event has mentioned yet
// is pending by definition. The graph (not the stream) says which nodes exist.
export function nodeView(run: RunView, id: string): NodeView {
  return run.nodes[id] ?? { id, status: 'pending', attempts: [], log: '', diagnosticCodes: [], activity: emptyActivity(), logDropped: 0 }
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
/** `code` is what the UI translates; `message` is the English text, kept for logs and tests. */
export type StreamEnd = { kind: 'complete' } | { kind: 'lost'; code: 'storage' | 'connection'; message: string }

export function classifyEnd(reason: string): StreamEnd {
  if (reason === 'end') return { kind: 'complete' }
  if (reason === 'server_error') return { kind: 'lost', code: 'storage', message: 'The engine stopped this run because it could not be stored.' }
  return { kind: 'lost', code: 'connection', message: 'The connection to the engine ended unexpectedly.' }
}

// A run that has no terminal event and is not being followed is interrupted, and
// so is every node that started and never finished (running, retrying or waiting
// for a human). A finished node keeps the status its events gave it.
export function settle(run: RunView, live: boolean): RunView {
  if (run.status !== 'running' || live || run.eventCount === 0) return run
  const nodes: Record<string, NodeView> = {}
  for (const [id, node] of Object.entries(run.nodes)) {
    nodes[id] = node.status === 'running' || node.status === 'waiting_human' ? { ...node, status: 'interrupted' } : node
  }
  return { ...run, status: 'interrupted', nodes }
}

// presentRun is what the inspector shows for a stream in a given end state:
// a normally ended log without a terminal event is "interrupted"; a stream that
// is still followed, or whose connection was lost, keeps the reduced status.
// storeStatus is the status the store reported for the run: "running" means a
// writer in another process still holds it, so a log without a terminal event is
// live there, not interrupted.
export function presentRun(run: RunView, endedNormally: boolean, lost: boolean, storeStatus?: string): RunView {
  return settle(run, !endedNormally || lost || storeStatus === 'running')
}

// streamLost is the lost-connection message worth showing, or null. Once the
// reduced events contain the run's terminal event there is nothing left to
// follow, so a later drop (before the server's end marker) is not a problem.
export function streamLost(run: RunView, lost: string | null): string | null {
  return run.status === 'running' ? lost : null
}

export function pendingApprovals(run: RunView): NodeView[] {
  return run.order.map((id) => run.nodes[id]!).filter((n) => n.status === 'waiting_human')
}
