import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import valueEvents from './fixtures/demo-approve-values.json'
import approveEvents from './fixtures/demo-approve.json'
import progressEvents from './fixtures/agent-progress.json'
import scopeEvents from './fixtures/agent-scope-retry.json'
import gateEvents from './fixtures/standalone-gate-red.json'
import { AttemptCompare } from '../src/AttemptCompare'
import { DataTab, prettyJson } from '../src/DataTab'
import { compareModel, defaultPair } from '../src/compare'
import { nodeData, type DataPort, type PortState } from '../src/dataModel'
import { I18nProvider } from '../src/i18n/context'
import { NodePanel } from '../src/NodePanel'
import { ActivityView } from '../src/nodeDetail'
import { MAX_ACTIVITY_ITEMS, MAX_ACTIVITY_TEXT, MAX_LOG_CHARS, nodeView, reduce, reduceAll, initialRun, type RunView } from '../src/reducer'
import { agentProgressGraph, agentScopeGraph, demoGraph, standaloneGateGraph } from '../src/storybook/fixtures'
import type { ApEvent, GraphSummary } from '../src/types'
import { initialValues, reduceAllValues, reduceValues, type RunValues } from '../src/values'

// Recorded fixtures: demo-approve (schema 9), demo-approve-values (schema 10, --record-values),
// agent-scope-retry and agent-progress (engine runtime with a fake adapter), standalone-gate-red.
const approve = approveEvents as unknown as ApEvent[]
const withValues = valueEvents as unknown as ApEvent[]
const progress = progressEvents as unknown as ApEvent[]
const scope = scopeEvents as unknown as ApEvent[]
const gate = gateEvents as unknown as ApEvent[]

type Locale = 'en' | 'pl'
function panel(events: ApEvent[], node: string, graph: GraphSummary | null, locale: Locale = 'en', tab?: string) {
  const run = reduceAll(events)
  const values = reduceAllValues(events[0]!.run_id, events)
  const kind = graph?.nodes.find((n) => n.id === node)?.type
  const html = renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <NodePanel run={run} nodeId={node} kind={kind} values={values} graph={graph} />
    </I18nProvider>,
  )
  return { run, values, html, tab }
}

const activityHtml = (run: RunView, node: string) =>
  renderToStaticMarkup(
    <I18nProvider locale="en">
      <ActivityView run={run} n={nodeView(run, node)} />
    </I18nProvider>,
  )

const slice = (events: ApEvent[], type: string, node?: string, n = 1) => {
  const matches = events.filter((e) => e.type === type && (node === undefined || e.node_id === node))
  return matches[n - 1]!
}

describe('standalone Gate (FR2) and check scope (FR3)', () => {
  it('shows the verdict, check ID, kind, duration, reason, evidence and checked inputs of the gate_check, not "no correction loop"', () => {
    const { html } = panel(gate, 'accept', standaloneGateGraph)
    const event = slice(gate, 'gate_check', 'accept')
    expect(html).toContain(String(event.payload.check_id))
    expect(html).toContain(String(event.payload.kind))
    expect(html).toContain(String(event.payload.reason))
    expect(html).toContain('data-testid="check-evidence"')
    expect(html).not.toContain('no correction loop')
    const inputs = (event.payload.scope as { checked_inputs?: string[] } | undefined)?.checked_inputs ?? []
    if (inputs.length > 0) expect(html).toContain(inputs.join(', '))
  })

  it('shows mode, observed count and allowed paths equal to the event\'s changed_files_scope', () => {
    const checks = scope.filter((e) => e.type === 'gate_check')
    const detail = checks[0]!.payload.changed_files_scope as { mode: string; allowed_paths: string[]; observed_count: number }
    const run = reduceAll(scope)
    const view = run.nodes.work!.attempts[0]!.check!
    expect(view.scope).toEqual({ mode: detail.mode, allowedPaths: detail.allowed_paths, redacted: false, observedCount: detail.observed_count })
    expect(view.checkedInputs).toEqual((checks[0]!.payload.scope as { checked_inputs: string[] }).checked_inputs)
    const { html } = panel(scope, 'work', agentScopeGraph)
    expect(html).toContain(`Mode: ${detail.mode}`)
    expect(html).toContain(detail.allowed_paths.join(', '))
    expect(html).toContain('data-testid="check-inputs"')
  })

  it('says "redacted" instead of the allowed paths when scope_redacted is set, and never shows them', () => {
    const redacted = scope.map((e) => (e.type === 'gate_check' ? { ...e, payload: { ...e.payload, changed_files_scope: { ...(e.payload.changed_files_scope as object), scope_redacted: true } } } : e))
    const { html, run } = panel(redacted, 'work', agentScopeGraph)
    expect(run.nodes.work!.attempts[0]!.check!.scope).toMatchObject({ redacted: true, allowedPaths: undefined })
    expect(html).toContain('Allowed paths: redacted')
    expect(html).toContain('data-testid="check-allowed-paths">Allowed paths: redacted<')
  })
})

describe('facts header (FR1) and attempt labels (FR4)', () => {
  it('shows the node-run ID, start, end, duration and adapter equal to the events', () => {
    const { run, html } = panel(progress, 'scout', agentProgressGraph)
    const n = run.nodes.scout!
    const started = slice(progress, 'node_started', 'scout')
    const evidence = slice(progress, 'node_evidence', 'scout')
    expect(n.nodeRunId).toBe(started.node_run_id)
    expect(html).toContain(String(started.node_run_id))
    expect(html).toContain(`${evidence.payload.adapter_id} ${evidence.payload.adapter_version}`)
    expect(html).toMatch(/data-testid="fact-duration">[^<]*ms</)
    expect(html).toContain('data-testid="fact-started"')
  })

  it('shows no duration for a node that is still running', () => {
    const { html } = panel(approve.slice(0, 8), 'fast', demoGraph)
    expect(html).toMatch(/data-testid="fact-duration">running</)
    expect(html).toMatch(/data-testid="fact-finished">running</)
  })

  it('names attempt tabs "Attempt N, outcome" in text in both languages', () => {
    const en = panel(scope, 'work', agentScopeGraph).html
    expect(en).toContain('Attempt 1, rejected')
    expect(en).toContain('Attempt 2, accepted')
    const pl = panel(scope, 'work', agentScopeGraph, 'pl').html
    expect(pl).toContain('Próba 1, rejected')
    expect(pl).toContain('Próba 2, accepted')
  })

  it('says an unfinished attempt is running', () => {
    const cut = scope.slice(0, scope.findIndex((e) => e.type === 'attempt_started' && e.payload.number === 2) + 1)
    expect(panel(cut, 'work', agentScopeGraph).html).toContain('Attempt 2, running')
  })
})

describe('changed files and diff summary (FR5-7)', () => {
  const [a1, a2] = ((): [string[], string[]] => {
    const run = reduceAll(scope)
    return [run.nodes.work!.attempts[0]!.changedFiles!, run.nodes.work!.attempts[1]!.changedFiles!]
  })()

  it('lists the observed paths of each attempt with the count and the "observed by Git" label', () => {
    expect(a1).toEqual(['outside.go', 'src/a.go'])
    expect(a2).toEqual(['outside.go', 'src/a.go', 'src/b.go'])
    const run = reduceAll(scope)
    for (const [i, files] of [[0, a1], [1, a2]] as const) {
      const attempt = run.nodes.work!.attempts[i]!
      const html = renderToStaticMarkup(
        <I18nProvider locale="en">
          <NodePanel run={{ ...run, nodes: { ...run.nodes, work: { ...run.nodes.work!, attempts: run.nodes.work!.attempts } } }} nodeId="work" kind="agent" />
        </I18nProvider>,
      )
      // The panel follows the latest attempt; the first attempt is read through the model.
      if (i === 1) {
        expect(html).toContain(`${files.length} files`)
        expect(html).toContain('observed by Git')
        for (const p of files) expect(html).toContain(p)
      }
      expect(attempt.changedFiles).toEqual(files)
    }
  })

  it('shows "not observed" with its reason for a Shell node, and never "0 files"', () => {
    const { html } = panel(approve, 'fast', demoGraph)
    expect(html).toContain('data-testid="changed-not-observed"')
    expect(html).toContain('Shell producers do not report changed files')
    expect(html).not.toMatch(/\b0 files\b/)
  })

  it('keeps node_evidence without an attempt ID on the node and on no attempt', () => {
    const run = reduceAll(progress)
    expect(run.nodes.scout!.changedFiles).toEqual(['src/a.go', 'src/b.go'])
    expect(run.nodes.scout!.attempts).toEqual([])
    const looped = reduceAll(scope)
    expect(looped.nodes.work!.changedFiles).toBeUndefined()
    expect(run.nodes.scout!.adapter).toEqual({ id: 'fake', version: '1.0.0' })
  })

  it('lists provider-reported files only under their own label, and the observed count does not depend on them', () => {
    const { html, run } = panel(progress, 'scout', agentProgressGraph)
    expect(html).toContain('reported by the agent (not verified)')
    const observed = /data-testid="changed-list">([\s\S]*?)<\/ul>/.exec(html)![1]!
    expect(observed).not.toContain('src/claimed.go')
    expect(/data-testid="reported-files">[\s\S]*src\/claimed\.go/.test(html)).toBe(true)
    const without = reduceAll(progress.filter((e) => e.type !== 'file_changed'))
    expect(without.nodes.scout!.changedFiles).toEqual(run.nodes.scout!.changedFiles)
    expect(panel(progress.filter((e) => e.type !== 'file_changed'), 'scout', agentProgressGraph).html).not.toContain('reported-files')
  })

  it('shows counts in the diff summary, says contents are not recorded, and has no diff rows in the DOM', () => {
    const { html } = panel(scope, 'work', agentScopeGraph)
    expect(html).toContain('data-testid="diff-summary"')
    expect(html).toContain('3 files observed')
    expect(html).toMatch(/\d+ out of scope according to the scope check/)
    expect(html).toContain('file contents are not recorded')
    expect(html).not.toMatch(/diff-row|class="diff|<ins|<del/)
  })
})

describe('activity (FR8, FR9)', () => {
  const run = reduceAll(progress)

  it('merges text by source and a tool call or command by ID, in stream order', () => {
    const items = run.nodes.scout!.activity.items
    expect(items.map((i) => i.kind)).toEqual(['text', 'tool', 'command', 'file'])
    expect(items[0]).toEqual({ kind: 'text', sourceId: 'msg-1', text: 'Looking at the failing test.' })
    expect(items[1]).toEqual({ kind: 'tool', callId: 'call-1', name: 'read_file', status: 'completed' })
    expect(items[2]).toEqual({ kind: 'command', commandId: 'cmd-1', status: 'completed', exitCode: 0 })
    expect(run.nodes.scout!.activity.dropped).toBe(0)
  })

  it('shows the items with a command exit code, and an unknown tool by its machine name', () => {
    const { html } = panel(progress, 'scout', agentProgressGraph)
    expect(html).toContain('data-testid="tab-activity"')
    const events = [...progress, { ...slice(progress, 'tool_call_finished'), sequence: 900, payload: { call_id: 'x', name: 'weird_tool', status: 'future_status' } }]
    const view = reduceAll(events).nodes.scout!.activity.items
    expect(view[view.length - 1]).toEqual({ kind: 'tool', callId: 'x', name: 'weird_tool', status: 'future_status' })
  })

  it('turns a tool_call_finished without a start into its own item, and keeps a start unfinished', () => {
    const solo = progress.filter((e) => e.type !== 'tool_call_started')
    expect(reduceAll(solo).nodes.scout!.activity.items.filter((i) => i.kind === 'tool')).toHaveLength(1)
    const cut = progress.filter((e) => e.type !== 'tool_call_finished')
    expect(reduceAll(cut).nodes.scout!.activity.items.find((i) => i.kind === 'tool')).toMatchObject({ status: 'in_progress' })
  })

  it('shows an in-progress tool as unfinished in an interrupted run', () => {
    const base = reduceAll(progress.filter((e) => e.type !== 'tool_call_finished' && e.type !== 'run_finished' && e.type !== 'node_finished'))
    expect(activityHtml(base, 'scout')).toContain('in_progress')
    expect(activityHtml({ ...base, status: 'interrupted' }, 'scout')).toMatch(/read_file<\/span> <span class="mu">unfinished/)
  })

  it('keeps progress_omitted as a marker with its counts and still as an EventLog row', () => {
    const omitted: ApEvent = { ...progress[0]!, sequence: 777, type: 'progress_omitted', node_id: 'scout', payload: { retained_events: 5, retained_bytes: 100, live_events: 7, live_bytes: 200, live_stopped: false } }
    const state = reduce(run, omitted)
    expect(state.nodes.scout!.activity.items.at(-1)).toMatchObject({ kind: 'omitted', retainedEvents: 5, liveEvents: 7 })
    expect(state.tail.at(-1)!.type).toBe('progress_omitted')
    expect(activityHtml(state, 'scout')).toContain('Some progress was not recorded (5 retained events, 7 live events).')
  })

  it('keeps normalized progress out of the EventLog tail while counting every event', () => {
    expect(run.tail.some((r) => ['text_delta', 'tool_call_started', 'tool_call_finished', 'command_started', 'command_finished', 'file_changed', 'status'].includes(r.type))).toBe(false)
    expect(run.eventCount).toBe(progress.length)
  })

  it('bounds a 10,000-event text stream to 200 items and 64 KiB, counts what it dropped and keeps the tail clean', () => {
    const base = { event_schema_version: 11, timestamp: '2026-01-01T00:00:00Z', run_id: 'r', graph_id: 'g', graph_version: 'v', node_id: 'n' }
    let state: RunView = initialRun()
    for (let i = 1; i <= 10000; i++) state = reduce(state, { ...base, type: 'text_delta', sequence: i, payload: { source_id: `s${i}`, text: 'x'.repeat(20) } })
    state = reduce(state, { ...base, type: 'progress_omitted', sequence: 10001, payload: { retained_events: 1, retained_bytes: 1, live_events: 1, live_bytes: 1, live_stopped: true } })
    const a = state.nodes.n!.activity
    const text = a.items.reduce((sum, i) => sum + (i.kind === 'text' ? i.text.length : 0), 0)
    expect(a.items.length).toBeLessThanOrEqual(MAX_ACTIVITY_ITEMS)
    expect(text).toBeLessThanOrEqual(MAX_ACTIVITY_TEXT)
    expect(a.dropped + a.items.length).toBe(10001)
    expect(state.tail.map((r) => r.type)).toEqual(['progress_omitted'])
    expect(state.eventCount).toBe(10001)
  })

  it('lists the stream\'s items in order in the Activity view', () => {
    const html = activityHtml(run, 'scout')
    const order = ['activity-text', 'activity-tool', 'activity-command', 'activity-file'].map((id) => html.indexOf(`data-testid="${id}"`))
    expect(order.every((i) => i >= 0)).toBe(true)
    expect([...order].sort((x, y) => x - y)).toEqual(order)
    expect(html).toContain('exit 0')
    expect(html).toContain('Looking at the failing test.')
  })

  it('says progress was not recorded for a node with none and for an old schema', () => {
    const none = reduceAll(approve)
    expect(activityHtml(none, 'fast')).toContain('No progress was recorded for this node.')
    const old = reduceAll(approve.map((e) => ({ ...e, event_schema_version: 2 })))
    expect(old.schemaVersion).toBe(2)
    expect(activityHtml(old, 'fast')).toContain('predates progress events')
  })
})

describe('attempt input summary (FR15)', () => {
  it('shows a 12-character fingerprint equal to the prefix of attempt_input and the intended scope', () => {
    const inputs = scope.filter((e) => e.type === 'attempt_input')
    const run = reduceAll(scope)
    const attempts = run.nodes.work!.attempts
    expect(attempts.map((a) => a.input?.fingerprint.slice(0, 12))).toEqual(inputs.map((e) => String(e.payload.task_fingerprint).slice(0, 12)))
    expect(attempts[0]!.input!.scope).toEqual(inputs[0]!.payload.intended_scope)
    const { html } = panel(scope, 'work', agentScopeGraph)
    expect(html).toContain(String(inputs[1]!.payload.task_fingerprint).slice(0, 12))
    expect(html).not.toContain(String(inputs[1]!.payload.task_fingerprint))
  })

  it('says "scope omitted" when the engine omitted the scope', () => {
    const omitted = scope.map((e) => (e.type === 'attempt_input' ? { ...e, payload: { ...e.payload, scope_omitted: true, intended_scope: [] } } : e))
    expect(panel(omitted, 'work', agentScopeGraph).html).toContain('Scope omitted')
  })

  it('adds a Task input row to Compare that states same or different from the two fingerprints', () => {
    const run = reduceAll(scope)
    const n = nodeView(run, 'work')
    const pair = defaultPair(n)!
    const model = compareModel(run, n, pair.from, pair.to, 'agent')!
    const [f1, f2] = n.attempts.map((a) => a.input?.fingerprint)
    expect(model.taskInput).toBe(f1 === f2 ? 'same' : 'different')
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <AttemptCompare run={run} node={n} kind="agent" from={pair.from} to={pair.to} onChange={() => undefined} />
      </I18nProvider>,
    )
    expect(html).toContain('data-testid="cmp-task-input"')
    expect(html).toContain(model.taskInput === 'same' ? 'same fingerprint' : 'different fingerprint')
    const same = scope.map((e) => (e.type === 'attempt_input' ? { ...e, payload: { ...e.payload, task_fingerprint: 'f'.repeat(64) } } : e))
    const runSame = reduceAll(same)
    expect(compareModel(runSame, nodeView(runSame, 'work'), pair.from, pair.to)!.taskInput).toBe('same')
    const none = reduceAll(scope.filter((e) => e.type !== 'attempt_input'))
    expect(compareModel(none, nodeView(none, 'work'), pair.from, pair.to)!.taskInput).toBe('unknown')
  })
})

describe('log marker (FR16)', () => {
  const chunk = (sequence: number, text: string): ApEvent => ({ event_schema_version: 11, type: 'log_chunk', timestamp: '2026-01-01T00:00:00Z', sequence, run_id: 'r', graph_id: 'g', graph_version: 'v', node_id: 'n', payload: { data: btoa(text) } })

  it('counts what the 64 KiB cap dropped and shows "earlier output not shown" only then', () => {
    const big = reduce(reduce(initialRun(), chunk(1, 'a'.repeat(MAX_LOG_CHARS))), chunk(2, 'b'.repeat(1000)))
    expect(big.nodes.n!.logDropped).toBe(1000)
    expect(big.nodes.n!.log.length).toBe(MAX_LOG_CHARS)
    const small = reduce(initialRun(), chunk(1, 'hello'))
    expect(small.nodes.n!.logDropped).toBe(0)
  })
})

// ---- Data tab ----

const valueRun = reduceAll(withValues)
const valuesOf = (events: ApEvent[]): RunValues => reduceAllValues(events[0]!.run_id, events)
const portsOf = (events: ApEvent[], graph: GraphSummary, node: string) => nodeData(valuesOf(events), graph, node)
const state = (ports: DataPort[], name: string): PortState => ports.find((p) => p.name === name)!.state

describe('Data tab states (FR12, FR13)', () => {
  it('shows each bound input\'s source value and each output\'s accepted value equal to the value_recorded payloads', () => {
    const data = portsOf(withValues, demoGraph, 'approve')
    const recorded = (node: string, port: string) => withValues.find((e) => e.type === 'value_recorded' && e.node_id === node && e.payload.port === port)!.payload
    const evidence = state(data.inputs, 'evidence')
    const summary = state(data.inputs, 'summary')
    const decision = state(data.outputs, 'decision')
    for (const [s, payload] of [[evidence, recorded('flaky', 'json')], [summary, recorded('fast', 'stdout')], [decision, recorded('approve', 'decision')]] as const) {
      expect(s.kind).toBe('recorded')
      if (s.kind !== 'recorded') continue
      expect(s.value).toMatchObject({ value: payload.value, type: payload.type, sizeBytes: payload.size_bytes, truncated: payload.truncated, redacted: payload.redacted })
    }
    expect(data.inputs.find((p) => p.name === 'summary')!.source).toBe('fast.stdout')
    const html = panel(withValues, 'approve', demoGraph).html
    expect(html).toBeTruthy()
  })

  it('reads "older run" for every value of the schema 9 demo-approve recording', () => {
    const data = portsOf(approve, demoGraph, 'approve')
    expect(data.inputs.map((p) => p.state.kind)).toEqual(['olderRun', 'olderRun'])
    expect(data.outputs.map((p) => p.state.kind)).toEqual(['olderRun'])
  })

  it('reads "not recorded" for a schema 11 run without value events, and links to Settings', () => {
    const modern = approve.map((e) => ({ ...e, event_schema_version: 11 }))
    const data = portsOf(modern, demoGraph, 'approve')
    expect(data.outputs[0]!.state.kind).toBe('notRecorded')
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={data} kind="human" onOpenSettings={() => undefined} />
      </I18nProvider>,
    )
    expect(html).toContain('data-testid="data-open-settings"')
    expect(html).toContain('Record run values')
  })

  it('reads "node not finished" for a source cut before its node_finished, and "failed or skipped" after a failure', () => {
    const cut = withValues.slice(0, withValues.findIndex((e) => e.type === 'node_finished' && e.node_id === 'flaky'))
    expect(state(portsOf(cut, demoGraph, 'approve').inputs, 'evidence').kind).toBe('notFinished')
    const failed = withValues.map((e) => (e.type === 'node_finished' && e.node_id === 'flaky' ? { ...e, payload: { ...e.payload, status: 'failed' } } : e))
    expect(state(portsOf(failed, demoGraph, 'approve').inputs, 'evidence').kind).toBe('failedOrSkipped')
    const skipped = withValues.map((e) => (e.type === 'node_finished' && e.node_id === 'flaky' ? { ...e, type: 'node_terminated', payload: { status: 'skipped' } } : e))
    expect(state(portsOf(skipped, demoGraph, 'approve').inputs, 'evidence').kind).toBe('failedOrSkipped')
  })

  it('reads "no value for this port" when the run has values but none for this key', () => {
    const without = withValues.filter((e) => !(e.type === 'value_recorded' && e.node_id === 'approve'))
    expect(state(portsOf(without, demoGraph, 'approve').outputs, 'decision').kind).toBe('noValue')
  })

  it('reads "value not carried" for an event without a value, with its size and hash', () => {
    const budget = withValues.map((e) => (e.type === 'value_recorded' && e.node_id === 'fast' ? { ...e, payload: { ...e.payload, value: undefined, truncated: true } } : e))
    const s = state(portsOf(budget, demoGraph, 'approve').inputs, 'summary')
    expect(s.kind).toBe('notCarried')
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={portsOf(budget, demoGraph, 'approve')} />
      </I18nProvider>,
    )
    expect(html).toContain('Value not carried')
    expect(html).toContain('truncated, no text carried')
    expect(html).toContain('sha256 ')
  })

  it('shows truncated and redacted marks and an empty string as an empty value', () => {
    const marked = withValues.map((e) =>
      e.type === 'value_recorded' && e.node_id === 'fast' ? { ...e, payload: { ...e.payload, truncated: true, redacted: true, size_bytes: 100 } } : e.type === 'value_recorded' && e.node_id === 'approve' ? { ...e, payload: { ...e.payload, value: '', size_bytes: 0 } } : e,
    )
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={portsOf(marked, demoGraph, 'approve')} />
      </I18nProvider>,
    )
    expect(html).toContain('first 16 of 100 bytes')
    expect(html).toContain('redacted')
    expect(html).toContain('data-testid="value-empty"')
    expect(html).not.toContain('Not recorded')
  })

  it('reads "optional input not bound" and "bindings unavailable" from the graph summary', () => {
    const optional: GraphSummary = { nodes: [{ id: 'n', type: 'shell', inputs: [{ name: 'note', type: 'string', required: false }, { name: 'job', type: 'string', required: true }], outputs: [] }], edges: [], bindings: [] }
    const data = nodeData(initialValues('r'), optional, 'n')
    expect(state(data.inputs, 'note').kind).toBe('optionalUnbound')
    expect(state(data.inputs, 'job').kind).toBe('unbound')
    const older: GraphSummary = { nodes: [{ id: 'n', type: 'shell', inputs: [{ name: 'a', type: 'string', required: true }], outputs: [] }], edges: [] }
    expect(state(nodeData(initialValues('r'), older, 'n').inputs, 'a').kind).toBe('bindingsUnavailable')
    // A response from an older engine (no new fields at all) parses and says so.
    const oldest = JSON.parse('{"nodes":[{"id":"n","type":"shell"}],"edges":[]}') as GraphSummary
    expect(nodeData(initialValues('r'), oldest, 'n').unavailable).toBe(true)
    expect(nodeData(initialValues('r'), null, 'n').unavailable).toBe(true)
  })

  it('shows ambiguity from two node runs for one key and guesses neither value', () => {
    const second = { ...withValues.find((e) => e.type === 'value_recorded' && e.node_id === 'fast')!, sequence: 500, node_run_id: 'another-run', payload: { ...withValues.find((e) => e.type === 'value_recorded' && e.node_id === 'fast')!.payload, node_run_id: 'another-run', value: 'second' } }
    const data = portsOf([...withValues, second], demoGraph, 'approve')
    const s = state(data.inputs, 'summary')
    expect(s.kind).toBe('ambiguous')
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={data} />
      </I18nProvider>,
    )
    expect(html).not.toContain('fast branch done')
    expect(html).not.toContain('>second<')
  })

  it('pretty-prints json collapsibly, starts values over 2 KiB collapsed, and shows an artifact_ref as a reference only', () => {
    const json = withValues.map((e) => (e.type === 'value_recorded' && e.node_id === 'flaky' ? { ...e, payload: { ...e.payload, value: '{"b":[1,2],"a":{"c":true}}' } } : e))
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={portsOf(json, demoGraph, 'approve')} />
      </I18nProvider>,
    )
    expect(html).toContain('<details')
    expect(html).toContain(JSON.stringify({ b: [1, 2], a: { c: true } }, null, 2).replace(/"/g, '&quot;'))
    expect(prettyJson('not json {')).toBe('not json {')
    const long = withValues.map((e) => (e.type === 'value_recorded' && e.node_id === 'fast' ? { ...e, payload: { ...e.payload, value: 'y'.repeat(3000), size_bytes: 3000 } } : e))
    const longHtml = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={portsOf(long, demoGraph, 'approve')} />
      </I18nProvider>,
    )
    expect(longHtml).toMatch(/<details data-testid="value-details"><summary>Long value/)
    const ref = withValues.map((e) => (e.type === 'value_recorded' && e.node_id === 'fast' ? { ...e, payload: { ...e.payload, type: 'artifact_ref', value: 'artifact:sha256:abc' } } : e))
    const refHtml = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={portsOf(ref, demoGraph, 'approve')} />
      </I18nProvider>,
    )
    expect(refHtml).toContain('artifact:sha256:abc')
    expect(refHtml).toContain('artifact contents are not stored')
    expect(refHtml).not.toContain('<a ')
  })

  it('renders a hostile recorded value as text and creates no element', () => {
    const hostile = '<img src=x onerror=alert(1)>'
    const events = withValues.map((e) => (e.type === 'value_recorded' && e.node_id === 'fast' ? { ...e, payload: { ...e.payload, value: hostile } } : e))
    const html = renderToStaticMarkup(
      <I18nProvider locale="en">
        <DataTab data={portsOf(events, demoGraph, 'approve')} />
      </I18nProvider>,
    )
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;')
  })

  it('says the raw task and command arguments are not recorded by design', () => {
    expect(renderToStaticMarkup(<I18nProvider locale="en"><DataTab data={portsOf(withValues, demoGraph, 'fast')} kind="shell" /></I18nProvider>)).toContain('Command arguments are not recorded by design.')
    expect(renderToStaticMarkup(<I18nProvider locale="en"><DataTab data={portsOf(progress, agentProgressGraph, 'scout')} kind="agent" /></I18nProvider>)).toContain('The resolved task is not recorded by design.')
  })
})

describe('live, replayed and duplicated delivery agree', () => {
  const detail = (run: RunView, values: RunValues, node: string, graph: GraphSummary) => ({ n: run.nodes[node], values, data: nodeData(values, graph, node) })
  const cases: [string, ApEvent[], string, GraphSummary][] = [
    ['values', withValues, 'approve', demoGraph],
    ['scope retry', scope, 'work', agentScopeGraph],
    ['progress', progress, 'scout', agentProgressGraph],
    ['standalone gate', gate, 'accept', standaloneGateGraph],
  ]
  for (const [name, events, node, graph] of cases) {
    it(`gives the same node detail model (${name})`, () => {
      const id = events[0]!.run_id
      const once = detail(reduceAll(events), reduceAllValues(id, events), node, graph)
      const cut = Math.floor(events.length / 2)
      const batched = detail(reduceAll(events.slice(cut), reduceAll(events.slice(0, cut))), events.slice(cut).reduce(reduceValues, reduceAllValues(id, events.slice(0, cut))), node, graph)
      const duplicated = detail(reduceAll(events, reduceAll(events.slice(0, cut + 1))), events.reduce(reduceValues, reduceAllValues(id, events.slice(0, cut + 1))), node, graph)
      expect(batched).toEqual(once)
      expect(duplicated).toEqual(once)
    })
  }

  it('the run view reducer does not reduce values (the run view is equal with and without them)', () => {
    const without = withValues.filter((e) => e.type !== 'value_recorded')
    const a = reduceAll(withValues)
    const b = reduceAll(without)
    expect({ ...a, eventCount: 0 }).toEqual({ ...b, eventCount: 0 })
    expect(valueRun.eventCount).toBe(withValues.length)
  })
})
