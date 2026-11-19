// Story data. Every run shown in a story is a REAL recorded event stream
// (test/fixtures/*.json, recorded with `agentplane run --json`); stories never
// build node/run state by hand. Truncating a stream at an event gives the state
// the engine was in at that moment.
import approve from '../../test/fixtures/demo-approve.json'
import reject from '../../test/fixtures/demo-reject.json'
import exhausted from '../../test/fixtures/exhausted.json'
import standaloneGate from '../../test/fixtures/standalone-gate-red.json'
import demoYaml from '../../../.agents/graphs/demo-vertical.yaml?raw'
import exhaustedYaml from '../../test/fixtures/exhausted.yaml?raw'
import standaloneGateYaml from '../../test/fixtures/standalone-gate-red.yaml?raw'
// Recorded from the engine runtime with the test fake Agent adapter (see the YAML header).
import approveValues from '../../test/fixtures/demo-approve-values.json'
import agentProgress from '../../test/fixtures/agent-progress.json'
import agentProgressYaml from '../../test/fixtures/agent-progress.yaml?raw'
import agentScope from '../../test/fixtures/agent-scope-retry.json'
import agentScopeYaml from '../../test/fixtures/agent-scope-retry.yaml?raw'
import { pendingApprovals, reduceAll } from '../reducer'
import type { ApEvent, GraphSummary, PendingApproval, RunSummary } from '../types'

export const demoApprove = approve as unknown as ApEvent[]
export const demoReject = reject as unknown as ApEvent[]
export const exhaustedRetries = exhausted as unknown as ApEvent[]
export const standaloneGateRed = standaloneGate as unknown as ApEvent[]
export const standaloneGateGraph: GraphSummary = { nodes: [{ id: 'build', type: 'shell' }, { id: 'accept', type: 'gate' }], edges: [{ from: 'build', to: 'accept' }] }
export { standaloneGateYaml }
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

// Recorded with `--record-values`: schema 10, the same graph as demoApprove.
export const demoApproveValues = approveValues as unknown as ApEvent[]
// Recorded from the engine runtime with a fake Agent executor that reports progress (agent-progress.yaml).
export const agentProgressRun = agentProgress as unknown as ApEvent[]
export { agentProgressYaml }
export const agentProgressGraph: GraphSummary = { nodes: [{ id: 'scout', type: 'agent', inputs: [], outputs: [] }], edges: [], bindings: [] }

const port = (name: string, type: string, required = true) => ({ name, type, required })
export const demoGraph: GraphSummary = {
  nodes: [
    { id: 'fast', type: 'shell', inputs: [], outputs: [port('stdout', 'string')] },
    { id: 'flaky', type: 'shell', max_attempts: 3, inputs: [], outputs: [port('json', 'json')] },
    { id: 'approve', type: 'human', inputs: [port('evidence', 'json'), port('summary', 'string')], outputs: [port('decision', 'string')] },
    { id: 'apply', type: 'shell', inputs: [port('decision', 'string')], outputs: [] },
  ],
  bindings: [
    { source: { node: 'flaky', port: 'json' }, target: { node: 'approve', input: 'evidence' } },
    { source: { node: 'fast', port: 'stdout' }, target: { node: 'approve', input: 'summary' } },
    { source: { node: 'approve', port: 'decision' }, target: { node: 'apply', input: 'decision' } },
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
