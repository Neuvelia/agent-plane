// Story data. Every run shown in a story is a REAL recorded event stream
// (test/fixtures/*.json, recorded with `agentplane run --json`); stories never
// build node/run state by hand. Truncating a stream at an event gives the state
// the engine was in at that moment.
import approve from '../../test/fixtures/demo-approve.json'
import reject from '../../test/fixtures/demo-reject.json'
import exhausted from '../../test/fixtures/exhausted.json'
import demoYaml from '../../../.agents/graphs/demo-vertical.yaml?raw'
import exhaustedYaml from '../../test/fixtures/exhausted.yaml?raw'
// Recorded from the engine runtime with the test fake Agent adapter (see the YAML header).
import agentScope from '../../test/fixtures/agent-scope-retry.json'
import agentScopeYaml from '../../test/fixtures/agent-scope-retry.yaml?raw'
import { pendingApprovals, reduceAll } from '../reducer'
import type { ApEvent, GraphSummary, PendingApproval, RunSummary } from '../types'

export const demoApprove = approve as unknown as ApEvent[]
export const demoReject = reject as unknown as ApEvent[]
export const exhaustedRetries = exhausted as unknown as ApEvent[]
export const agentScopeRetry = agentScope as unknown as ApEvent[]
export const agentScopeGraph: GraphSummary = { nodes: [{ id: 'work', type: 'agent', max_attempts: 3 }], edges: [] }
export { agentScopeYaml }
// The same run with attempt 2's check replaced by the payload the engine emits in
// warn mode when Git inspection fails (internal/gate/changed_files.go
// indeterminateChangedFiles): green, but the scope was not determined.
const lastScopeCheck = agentScopeRetry.filter((e) => e.type === 'gate_check')[1]
export const agentScopeIndeterminate: ApEvent[] = agentScopeRetry.map((e) =>
  e === lastScopeCheck
    ? {
        ...e,
        payload: {
          check_result_version: 1,
          scope: { checked_inputs: ['candidate'] },
          duration_ns: 0,
          check_id: 'scope',
          kind: 'changed_files_scope',
          verdict: 'green',
          reason: 'changed_files_scope_indeterminate',
          evidence: [{ kind: 'changed_files_scope', reference: { kind: 'diff', state: 'unavailable', reason: 'git_inspection_failed' }, source: 'git' }],
          changed_files_scope: { mode: 'warn', allowed_paths: ['src/'], scope_redacted: false, baseline_head: 'unavailable', observed_count: 0 },
        },
      }
    : e,
)

export const demoGraph: GraphSummary = {
  nodes: [
    { id: 'fast', type: 'shell' },
    { id: 'flaky', type: 'shell', max_attempts: 3 },
    { id: 'approve', type: 'human' },
    { id: 'apply', type: 'shell' },
  ],
  edges: [
    { from: 'fast', to: 'approve' },
    { from: 'flaky', to: 'approve' },
    { from: 'approve', to: 'apply' },
  ],
}
export const exhaustedGraph: GraphSummary = { nodes: [{ id: 'flaky', type: 'shell', max_attempts: 3 }], edges: [] }

export { demoYaml, exhaustedYaml }

/** The stream up to and including the first event matching `pred`. */
export function until(events: ApEvent[], pred: (e: ApEvent) => boolean): ApEvent[] {
  const i = events.findIndex(pred)
  if (i < 0) throw new Error('fixture has no matching event')
  return events.slice(0, i + 1)
}

export const waitingForHuman = until(demoApprove, (e) => e.type === 'approval_requested')
export const runningParallel = until(demoApprove, (e) => e.type === 'attempt_started' && e.sequence > 6)

export function summaryOf(events: ApEvent[], status: string): RunSummary {
  const first = events[0]!
  return { run_id: first.run_id, graph_id: first.graph_id, started_at: first.timestamp, status, event_count: events.length }
}

/** Pending approvals as the API would list them, derived from the event stream. */
export function approvalsOf(events: ApEvent[]): PendingApproval[] {
  const run = reduceAll(events)
  return pendingApprovals(run).flatMap((n) =>
    n.approval && n.nodeRunId && run.runId ? [{ run_id: run.runId, node_id: n.id, node_run_id: n.nodeRunId, prompt: n.approval.prompt, context: n.approval.context }] : [],
  )
}
