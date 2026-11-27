import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import agentEvents from './fixtures/agent-scope-retry.json'
import { AttemptCompare } from '../src/AttemptCompare'
import { NodePanel } from '../src/NodePanel'
import { canCompare, compareModel, defaultPair } from '../src/compare'
import { I18nProvider } from '../src/i18n/context'
import { nodeView, reduce, reduceAll, settle, type RunView } from '../src/reducer'
import type { ApEvent } from '../src/types'

// demo-approve.json: recorded `agentplane run .agents/graphs/demo-vertical.yaml --approve approve --json`;
// its "flaky" node is a Shell loop, red on attempt 1 and green on attempt 2.
// agent-scope-retry.json: recorded from the engine runtime with the test fake Agent
// adapter and a changed_files_scope deny Gate by internal/runtime/desktop_fixture_test.go
// (see agent-scope-retry.yaml).
const shell = approveEvents as unknown as ApEvent[]
const agent = agentEvents as unknown as ApEvent[]

const of = (events: ApEvent[], type: string, node: string) => events.filter((e) => e.type === type && e.node_id === node)

function model(events: ApEvent[], node: string, kind: string, run: RunView = reduceAll(events)) {
  const n = nodeView(run, node)
  const pair = defaultPair(n)!
  return { run, n, pair, m: compareModel(run, n, pair.from, pair.to, kind)! }
}

function html(run: RunView, node: string, kind: string, from?: string, to?: string, locale: 'en' | 'pl' = 'en') {
  const n = nodeView(run, node)
  const pair = defaultPair(n)!
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <AttemptCompare run={run} node={n} kind={kind} from={from ?? pair.from} to={to ?? pair.to} onChange={() => undefined} />
    </I18nProvider>,
  )
}

describe('entry point', () => {
  it('a node with two attempts can be compared; nodes with fewer cannot [F2.8-AC1]', () => {
    const run = reduceAll(shell)
    expect(canCompare(nodeView(run, 'flaky'))).toBe(true)
    expect(canCompare(nodeView(run, 'fast'))).toBe(false)
    const panel = (id: string) =>
      renderToStaticMarkup(
        <I18nProvider locale="en">
          <NodePanel run={run} nodeId={id} kind="shell" />
        </I18nProvider>,
      )
    expect(panel('flaky')).toContain('data-testid="tab-compare"')
    expect(panel('fast')).not.toContain('data-testid="tab-compare"')
  })
})

describe('shell loop recording', () => {
  const { n, pair, m } = model(shell, 'flaky', 'shell')
  const starts = of(shell, 'attempt_started', 'flaky')
  const finishes = of(shell, 'attempt_finished', 'flaky')
  const checks = of(shell, 'gate_check', 'flaky')

  it('defaults to From = the rejected attempt 1 and To = the accepted attempt 2', () => {
    expect(n.attempts.map((a) => a.number)).toEqual([1, 2])
    expect(pair).toEqual({ from: starts[0]!.attempt_id, to: starts[1]!.attempt_id })
    expect(m.from.outcome).toBe('rejected')
    expect(m.to.outcome).toBe('accepted')
    expect(m.to.accepted).toBe(true)
  })

  it('shows each attempt’s check exactly as its gate_check event recorded it', () => {
    for (const [side, event] of [
      [m.from, checks[0]!],
      [m.to, checks[1]!],
    ] as const) {
      const p = event.payload as Record<string, unknown>
      expect(side.check?.checkId).toBe(p.check_id)
      expect(side.check?.verdict).toBe(p.verdict)
      expect(side.check?.reason).toBe(p.reason)
      expect(side.check?.durationMs).toBe(Math.round((p.duration_ns as number) / 1e6))
      expect(side.check?.evidence.map((e) => [e.kind, e.source])).toEqual((p.evidence as Record<string, unknown>[]).map((e) => [e.kind, e.source]))
    }
  })

  it('attempt duration is the timestamp difference of attempt_started and attempt_finished', () => {
    for (const [i, side] of [m.from, m.to].entries()) expect(side.attemptMs).toBe(Date.parse(finishes[i]!.timestamp) - Date.parse(starts[i]!.timestamp))
  })

  it('says changed files were not observed for a Shell producer, and shows no new-file badge', () => {
    expect(m.from.files).toBeUndefined()
    expect(m.from.notObserved).toBe('shell')
    expect(m.to.notObserved).toBe('shell')
    const out = html(reduceAll(shell), 'flaky', 'shell')
    expect(out).toContain('Not observed: Shell producers do not report changed files.')
    expect(out).not.toContain('badge-new')
    expect(out).not.toContain('0 files')
  })

  it('reports the scope as not checked without a changed_files_scope Gate', () => {
    expect(m.from.scope).toEqual({ kind: 'not_checked' })
    expect(html(reduceAll(shell), 'flaky', 'shell')).toContain('Scope not checked')
  })

  it('shows the feedback that started attempt 2: attempt 1’s check, linked by attempt_input', () => {
    const input = of(shell, 'attempt_input', 'flaky')[1]!.payload as Record<string, unknown>
    expect(m.to.feedbackFrom).toBe(1)
    expect(m.from.feedbackFrom).toBeUndefined()
    expect(m.feedback?.fromNumber).toBe(1)
    expect(m.feedback?.previousCheckId).toBe(input.previous_check_id)
    expect(m.feedback?.check?.reason).toBe((checks[0]!.payload as Record<string, unknown>).reason)
    const out = html(reduceAll(shell), 'flaky', 'shell')
    expect(out).toContain('Feedback that started attempt 2')
    expect(out).toContain(`Previous check: ${String(input.previous_check_id)}`)
  })

  it('says file contents were not recorded and renders no diff', () => {
    const out = html(reduceAll(shell), 'flaky', 'shell')
    expect(out).toContain('File contents were not recorded for this run')
    expect(out).not.toMatch(/diff-|class="dl /)
  })

  it('swap exchanges every column', () => {
    const run = reduceAll(shell)
    const swapped = compareModel(run, n, pair.to, pair.from, 'shell')!
    expect(swapped.from).toEqual(m.to)
    expect(swapped.to).toEqual(m.from)
    const out = html(run, 'flaky', 'shell', pair.to, pair.from)
    expect(out.indexOf('data-testid="cmp-head-from">Attempt 2')).toBeGreaterThan(-1)
    expect(out.indexOf('data-testid="cmp-head-to">Attempt 1')).toBeGreaterThan(-1)
  })

  it('is localised in Polish', () => {
    const out = html(reduceAll(shell), 'flaky', 'shell', undefined, undefined, 'pl')
    expect(out).toContain('Zakresu nie sprawdzono')
    expect(out).toContain('Zawartość plików nie została zapisana')
    expect(out).not.toContain('Scope not checked')
  })
})

describe('agent loop with a changed_files_scope Gate', () => {
  const { m } = model(agent, 'work', 'agent')

  it('lists each attempt’s observed paths from its own node_evidence', () => {
    expect(m.from.files?.map((f) => f.path)).toEqual(['outside.go', 'src/a.go'])
    expect(m.to.files?.map((f) => f.path)).toEqual(['outside.go', 'src/a.go', 'src/b.go'])
  })

  it('marks only src/b.go as new in attempt 2', () => {
    expect(m.to.files?.filter((f) => f.isNew).map((f) => f.path)).toEqual(['src/b.go'])
    expect(m.from.files?.some((f) => f.isNew)).toBe(false)
    const out = html(reduceAll(agent), 'work', 'agent')
    expect((out.match(/data-testid="badge-new"/g) ?? []).length).toBe(1)
    expect(out).toContain('not in attempt 1')
  })

  it('marks outside.go outside allowed scope in attempt 1 from the engine’s check evidence only', () => {
    expect(m.from.scope).toEqual({ kind: 'checked', paths: ['outside.go'], omitted: 0 })
    expect(m.from.files?.filter((f) => f.outsideScope).map((f) => f.path)).toEqual(['outside.go'])
    expect(m.to.scope).toEqual({ kind: 'checked', paths: [], omitted: 0 })
    expect(m.to.files?.some((f) => f.outsideScope)).toBe(false)
    const out = html(reduceAll(agent), 'work', 'agent')
    expect((out.match(/data-testid="badge-outside"/g) ?? []).length).toBe(1)
    expect(out).toContain('All changes within allowed scope')
  })
})

// The exact gate_check payload internal/gate/changed_files.go indeterminateChangedFiles
// emits in warn mode when Git inspection fails: green, but nothing was checked.
const indeterminatePayload = {
  check_result_version: 1,
  scope: { checked_inputs: ['candidate'] },
  duration_ns: 0,
  check_id: 'scope',
  kind: 'changed_files_scope',
  verdict: 'green',
  reason: 'changed_files_scope_indeterminate',
  evidence: [{ kind: 'changed_files_scope', reference: { kind: 'diff', state: 'unavailable', reason: 'git_inspection_failed' }, source: 'git' }],
  changed_files_scope: { mode: 'warn', allowed_paths: ['src/'], scope_redacted: false, baseline_head: 'unavailable', observed_count: 0 },
}

describe('indeterminate scope check', () => {
  const last = agent.filter((e) => e.type === 'gate_check')[1]!
  const edited = agent.map((e) => (e === last ? { ...e, payload: indeterminatePayload } : e))
  const { m } = model(edited, 'work', 'agent')

  it('keeps the evidence reference and reports the scope as undetermined, not clean', () => {
    expect(m.to.check?.evidence[0]?.reference).toEqual({ kind: 'diff', state: 'unavailable', reason: 'git_inspection_failed' })
    expect(m.to.scope).toEqual({ kind: 'indeterminate' })
    expect(m.to.files?.some((f) => f.outsideScope)).toBe(false)
    const out = html(reduceAll(edited), 'work', 'agent')
    expect(out).toContain('Scope could not be determined')
    expect(out).toContain('data-testid="scope-indeterminate"')
    expect(out.match(/All changes within allowed scope/g)).toBeNull()
    expect(html(reduceAll(edited), 'work', 'agent', undefined, undefined, 'pl')).toContain('Nie udało się ustalić zakresu')
  })

  it('an evidence limit reference is still a checked scope with omitted paths', () => {
    const limited = { ...indeterminatePayload, verdict: 'red', reason: 'changed_files_outside_scope', evidence: [{ kind: 'changed_files_scope', source: 'git', path: 'outside.go' }, { kind: 'changed_files_scope', source: 'git', omitted: 4, reference: { kind: 'diff', state: 'unavailable', reason: 'paths_exceed_evidence_limit' } }] }
    const ev = agent.map((e) => (e === last ? { ...e, payload: limited } : e))
    expect(model(ev, 'work', 'agent').m.to.scope).toEqual({ kind: 'checked', paths: ['outside.go'], omitted: 4 })
  })
})

describe('unknown is unknown', () => {
  it('a missing feedback reference is reported, not invented', () => {
    const input = agent.find((e) => e.type === 'attempt_input' && (e.payload as Record<string, unknown>).previous_attempt_id)!
    const edited = agent.map((e) => (e === input ? { ...e, payload: { ...e.payload, previous_attempt_id: 'gone', previous_check_id: 'gone' } } : e))
    const { m } = model(edited, 'work', 'agent')
    expect(m.to.feedbackFrom).toBe(0)
    expect(m.feedback?.fromNumber).toBeUndefined()
    expect(m.feedback?.check).toBeUndefined()
    expect(html(reduceAll(edited), 'work', 'agent')).toContain('The referenced check result is not in this run’s events.')
  })

  it('an Agent attempt without node_evidence is not observed, not zero files', () => {
    const second = agent.filter((e) => e.type === 'attempt_started')[1]!.attempt_id
    const edited = agent.filter((e) => !(e.type === 'node_evidence' && e.attempt_id === second))
    const { m } = model(edited, 'work', 'agent')
    expect(m.to.files).toBeUndefined()
    expect(m.to.notObserved).toBe('not_recorded')
    expect(m.from.files?.some((f) => f.isNew)).toBe(false)
  })

  it('an unfinished attempt is running while the run is live and interrupted once it is not', () => {
    const cut = agent.slice(0, agent.findIndex((e) => e.type === 'gate_check' && e.sequence > 10))
    const live = model(cut, 'work', 'agent')
    expect(live.m.to.outcome).toBeUndefined()
    expect(live.m.to.attemptMs).toBeUndefined()
    expect(live.m.to.notObserved).toBe('running')
    const stopped = model(cut, 'work', 'agent', settle(reduceAll(cut), false))
    expect(stopped.m.to.outcome).toBe('interrupted')
    expect(stopped.m.to.notObserved).toBe('not_recorded')
  })
})

describe('stored and live reduce to the same compare model', () => {
  it('replay-then-follow with a reconnect equals reducing the stored log once', () => {
    const stored = model(agent, 'work', 'agent').m
    let live = reduceAll(agent.slice(0, 9))
    for (const e of agent) live = reduce(live, e) // replayed prefix is ignored by sequence
    const followed = model(agent, 'work', 'agent', live).m
    expect(followed).toEqual(stored)
  })
})
