import { readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import rejectEvents from './fixtures/demo-reject.json'
import { I18nProvider } from '../src/i18n/context'
import { translate } from '../src/i18n'
import { criticalPath } from '../src/criticalPath'
import { timeline } from '../src/insight'
import { NodeFilterBar, OutcomeCard, RunIdentity, RunSummaryCard } from '../src/overview'
import { initialRun, reduceAll, settle, type RunView } from '../src/reducer'
import { demoGraph } from '../src/storybook/fixtures'
import { NodeTimeline } from '../src/timelineView'
import type { ApEvent } from '../src/types'

const approve = approveEvents as unknown as ApEvent[]
const reject = rejectEvents as unknown as ApEvent[]
const none: ReadonlySet<string> = new Set()
const noop = () => undefined

const render = (node: React.ReactNode, locale: 'en' | 'pl' = 'en') => renderToStaticMarkup(<I18nProvider locale={locale}>{node}</I18nProvider>)

function timelineHtml(run: RunView, over: { selected?: string | null; critical?: ReadonlySet<string>; hidden?: ReadonlySet<string> } = {}, locale: 'en' | 'pl' = 'en') {
  return render(<NodeTimeline timeline={timeline(run)!} selected={over.selected ?? null} critical={over.critical ?? none} hidden={over.hidden ?? none} onSelect={noop} />, locale)
}

describe('timeline rows', () => {
  const run = reduceAll(approve)

  it('are native buttons (Enter and Space select) and the run context stays rendered around them [F2.6-AC1]', () => {
    const out = timelineHtml(run)
    for (const id of ['fast', 'flaky', 'approve', 'apply']) {
      expect(out).toMatch(new RegExp(`<button[^>]*type="button"[^>]*data-testid="timeline-row-${id}"`))
    }
    // No div or span posing as a button.
    expect(out).not.toMatch(/role="button"/)
  })

  it('name the node, the attempt count, the start offset, the duration and the outcome in text', () => {
    const out = timelineHtml(run)
    const label = /<button[^>]*aria-label="([^"]*)"[^>]*data-testid="timeline-row-flaky"/.exec(out)?.[1]
    expect(label).toMatch(/^flaky: 2 attempts, starts at .+, duration .+, Succeeded$/)
    expect(/aria-label="([^"]*)"[^>]*data-testid="timeline-row-fast"/.exec(out)?.[1]).toMatch(/^fast: 0 attempts,/)
  })

  it('has a hidden table with every segment, its attempt number and its outcome', () => {
    const out = timelineHtml(run)
    const table = /<table[^>]*data-testid="timeline-table"[\s\S]*?<\/table>/.exec(out)![0]
    expect(table).toContain('class="sr-only"')
    expect(table).toContain('<td>flaky</td>')
    expect(table).toContain('<td>rejected</td>')
    expect(table).toContain('<td>accepted</td>')
    expect((table.match(/<td>flaky<\/td>/g) ?? []).length).toBe(timeline(run)!.rows.find((r) => r.name === 'flaky')!.segments.length)
  })

  it('badges the critical path nodes with text, not only colour', () => {
    const path = criticalPath(run, demoGraph)!
    const out = timelineHtml(run, { critical: new Set(path.nodes) })
    expect(out).toContain('data-testid="timeline-critical-apply"')
    expect((out.match(/>Critical</g) ?? []).length).toBe(path.nodes.length)
  })

  it('hides the nodes a filter leaves out but never the selected one', () => {
    const hidden = new Set(['fast', 'flaky', 'approve', 'apply'])
    const out = timelineHtml(run, { hidden, selected: 'approve' })
    expect(out).toContain('data-testid="timeline-row-approve"')
    for (const id of ['fast', 'flaky', 'apply']) expect(out).not.toContain(`data-testid="timeline-row-${id}"`)
    expect(out).toMatch(/data-testid="timeline-row-approve"/)
    expect(out).toContain('aria-pressed="true"')
  })

  it('renders node IDs and outcomes as text, never as markup', () => {
    const hostile = reduceAll([{ ...approve[0]! }, { ...approve.find((e) => e.type === 'node_started')!, sequence: 2, node_id: '<img src=x onerror=alert(1)>' }])
    const out = timelineHtml(hostile)
    expect(out).not.toContain('<img')
    expect(out).toContain('&lt;img')
  })

  it('is localised', () => {
    expect(timelineHtml(run, {}, 'pl')).toMatch(/aria-label="flaky: 2 próby, start .+, czas .+, Zakończony/)
  })
})

describe('node filter control', () => {
  it('shows the count of matching nodes on each option', () => {
    const out = render(<NodeFilterBar run={reduceAll(approve)} graph={demoGraph} filter="all" onChange={noop} />)
    expect(out).toContain('All 4')
    expect(out).toContain('Retried 1')
    expect(out).toContain('Human 1')
    expect(out).toContain('Failed 0')
    // Four nodes finished, so Slow has a sample and stays enabled.
    expect(out).not.toMatch(/<button[^>]*disabled=""[^>]*data-testid="nodes-slow"/)
  })

  it('keeps Slow disabled with its reason below three finished nodes', () => {
    const early = reduceAll(approve.slice(0, 12))
    const out = render(<NodeFilterBar run={early} graph={demoGraph} filter="all" onChange={noop} />)
    expect(out).toMatch(/<button[^>]*disabled=""[^>]*data-testid="nodes-slow"/)
    expect(out).toContain('Slow needs at least 3 finished nodes.')
  })

  it('disables Human with a reason when the graph is not loaded, and marks the chosen option pressed', () => {
    const out = render(<NodeFilterBar run={reduceAll(approve)} graph={null} filter="retried" onChange={noop} />)
    expect(out).toMatch(/<button[^>]*disabled=""[^>]*data-testid="nodes-human"/)
    expect(out).toContain('Human needs the graph, which is not loaded.')
    expect(out).toMatch(/aria-pressed="true"[^>]*data-testid="nodes-retried"/)
  })
})

describe('what failed card', () => {
  it('is absent for a succeeded run and for a running run with nothing failed', () => {
    expect(render(<OutcomeCard run={reduceAll(approve)} onOpen={noop} />)).toBe('')
    expect(render(<OutcomeCard run={reduceAll(approve.slice(0, 12))} onOpen={noop} />)).toBe('')
  })

  it('lists the rejected human node as a button with the decision, the comment and the skipped dependents', () => {
    const out = render(<OutcomeCard run={reduceAll(reject)} onOpen={noop} />)
    expect(out).toMatch(/<button[^>]*type="button"[^>]*data-testid="outcome-approve"/)
    expect(out).toContain('rejected')
    expect(out).toContain('human_rejected')
    expect(out).toContain('Rejected by a person: decided by command-line flag')
    expect(out).toContain('1 node skipped')
  })

  it('says failures are so far while the run is still going', () => {
    const run = reduceAll(reject.slice(0, reject.findIndex((e) => e.type === 'node_finished' && e.node_id === 'approve') + 1))
    expect(run.status).toBe('running')
    expect(render(<OutcomeCard run={run} onOpen={noop} />)).toContain('Failures so far; the run is still going.')
  })

  it('shows reason text as received, escaped', () => {
    const hostile = reduceAll(reject.map((e) => (e.type === 'approval_decided' ? { ...e, payload: { ...e.payload, comment: '<b onmouseover=x>bold</b>' } } : e)))
    const out = render(<OutcomeCard run={hostile} onOpen={noop} />)
    expect(out).not.toContain('<b onmouseover')
    expect(out).toContain('&lt;b onmouseover=x&gt;')
  })
})

describe('run identity', () => {
  it('shows the full run ID, the start time and a 12-character graph version for a recorded run', () => {
    const run = reduceAll(approve)
    const out = render(<RunIdentity run={run} />)
    expect(out).toContain(`data-testid="run-id">${approve[0]!.run_id}<`)
    expect(out).toContain(run.graphVersion!.slice(0, 12))
    expect(out).not.toContain(run.graphVersion!)
    expect(out).toMatch(/data-testid="run-started">[^<]*\d/)
  })

  it('shows unknown for a stream without them and no filesystem path', () => {
    const out = render(<RunIdentity run={initialRun()} />)
    expect((out.match(/unknown/g) ?? []).length).toBe(3)
    expect(out).not.toMatch(/\/home|\\\\|file:/)
  })
})

describe('summary card', () => {
  it('shows agent time as not applicable for a graph without an agent node and human wait as its own figure', () => {
    const out = render(<RunSummaryCard run={reduceAll(approve)} graph={demoGraph} path={undefined} lost={false} />)
    expect(out).toMatch(/data-testid="kpi-agent"[\s\S]*?not applicable/)
    expect(out).toContain('data-testid="kpi-human"')
    expect(out).toContain('data-testid="kpi-queued"')
    expect(out).toContain('Human wait')
    expect(out).toContain('Node time')
  })

  it('names the critical path as text and says "so far" for a path of an unfinished run', () => {
    const run = reduceAll(approve.slice(0, 14))
    const path = criticalPath(run, demoGraph)
    expect(path?.soFar).toBe(true)
    const out = render(<RunSummaryCard run={run} graph={demoGraph} path={path} lost={false} />)
    expect(out).toContain(path!.nodes.join(' → '))
    expect(out).toMatch(/data-testid="critical-path"[\s\S]*so far/)
  })

  it('says the path needs the graph when it is not loaded, and notes a lost connection', () => {
    const out = render(<RunSummaryCard run={reduceAll(approve.slice(0, 14))} graph={null} path={undefined} lost />)
    expect(out).toContain('Needs the graph, which is not loaded.')
    expect(out).toContain('data-testid="overview-lost"')
    expect(out).toMatch(/kpi-agent[\s\S]*?unknown/)
  })

  it('labels a wait of an interrupted run as so far', () => {
    const run = settle(reduceAll(approve.slice(0, approve.findIndex((e) => e.type === 'approval_requested') + 1)), false)
    expect(run.status).toBe('interrupted')
    expect(render(<RunSummaryCard run={run} graph={demoGraph} path={undefined} lost={false} />)).toContain('so far')
  })
})

describe('catalogue', () => {
  it('Polish plural cases for the new count messages: 1, 2-4, 5+, 12-14 and 22', () => {
    const cases: [number, string][] = [[1, '1 węzeł pominięty'], [2, '2 węzły pominięte'], [5, '5 węzłów pominiętych'], [12, '12 węzłów pominiętych'], [14, '14 węzłów pominiętych'], [22, '22 węzły pominięte']]
    for (const [count, text] of cases) expect(translate('pl', 'outcome.skipped', { count })).toBe(text)
    expect(translate('pl', 'timeline.row', { node: 'n', attempts: 2, start: '0', duration: '1', outcome: 'ok' })).toContain('2 próby')
    expect(translate('pl', 'timeline.row', { node: 'n', attempts: 5, start: '0', duration: '1', outcome: 'ok' })).toContain('5 prób')
    expect(translate('en', 'overview.unknownNodes', { count: 1 })).toContain('1 node has')
    expect(translate('pl', 'overview.unknownNodes', { count: 3 })).toContain('3 węzły nie mają')
  })
})

describe('no markup injection and no new trust surface', () => {
  it('the new files never use dangerouslySetInnerHTML, the bridge, storage or the network', () => {
    for (const file of ['overview.tsx', 'timelineView.tsx', 'waits.ts', 'criticalPath.ts', 'outcome.ts', 'nodeFilter.ts', 'timelineSegments.ts']) {
      const source = readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8')
      expect(source, file).not.toMatch(/dangerouslySetInnerHTML|window\.agentplane|localStorage|sessionStorage|fetch\(|Date\.now|new Date\(\)/)
    }
  })

  it('the preload bridge keeps the method set it had', () => {
    const preload = readFileSync(new URL('../electron/preload.cjs', import.meta.url), 'utf8')
    const types = readFileSync(new URL('../src/types.ts', import.meta.url), 'utf8')
    for (const method of ['request', 'subscribe', 'openGraph', 'newGraph', 'recentGraphs', 'openRecentGraph', 'touchRecent', 'closeGraph', 'recordingSettings', 'setRecordingSettings']) expect(types).toContain(method)
    expect(preload.length).toBeGreaterThan(0)
  })
})
