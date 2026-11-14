import { useEffect, useState } from 'react'
import { listApprovals, listRuns, startRun, validateGraph, type ApiFailure } from './api'
import { GraphMap } from './GraphMap'
import { useRunStream, usePoll } from './hooks'
import { formatDateTime, type Locale } from './i18n'
import { useI18n } from './i18n/context'
import { ApprovalBox, NodePanel } from './NodePanel'
import { nodeView, displayStatus, type RunView } from './reducer'
import { filterEvents, formatDuration, runKpis, timeline, type EventFilter } from './insight'
import { RunPill } from './status'
import type { Diagnostic } from './types'
import { CodeView, DotTimeline, EventLog, type EventRow } from './ui/data'
import { canStart, preflightState } from './preflight'
import { Button, Card, Field, HelpTip, KPI, ListRow, Pill, SegmentedControl, Step, Tag } from './ui/primitives'

// R1: Run Inspector, Approvals and New run on the design system. Behaviour and
// data are unchanged from F2.5; the Dashboard is redesigned in R2.

export function RunsList({ onOpen }: { onOpen: (id: string) => void }) {
  const { t, locale } = useI18n()
  const runs = usePoll(listRuns, [], 1500)
  return (
    <section>
      <h1>{t('runs.title')}</h1>
      {runs.length === 0 ? (
        <div className="empty-state" data-testid="no-runs">
          {t('runs.empty')}
        </div>
      ) : (
        <table className="runs" data-testid="runs-table">
          <thead>
            <tr>
              <th>{t('runs.col.status')}</th>
              <th>{t('runs.col.graph')}</th>
              <th>{t('runs.col.started')}</th>
              <th>{t('runs.col.events')}</th>
              <th>{t('runs.col.run')}</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => (
              <tr key={r.run_id} onClick={() => onOpen(r.run_id)} data-testid="run-row">
                <td>
                  <RunPill status={r.status} />
                </td>
                <td>{r.graph_id}</td>
                <td className="mono">{formatDateTime(locale, r.started_at)}</td>
                <td className="mono">{r.event_count}</td>
                <td className="mono mu">{r.run_id.slice(0, 8)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

export type GraphView = 'map' | 'yaml' | 'split'

const time = (locale: Locale, iso: string) => new Intl.DateTimeFormat(locale, { timeStyle: 'medium' }).format(new Date(iso))

function RunSummaryCard({ run }: { run: RunView }) {
  const { t, locale } = useI18n()
  const k = runKpis(run)
  const dur = (v: number | undefined) => (v === undefined ? undefined : formatDuration(v, locale))
  const unknown = t('kpi.unknown')
  return (
    <Card title={t('run.summary')} action={<HelpTip>{t('run.summaryHelp')}</HelpTip>} testId="run-summary">
      <div className="kpis">
        <KPI label={t('kpi.wall')} value={dur(k.wallClockMs)} unknownLabel={unknown} />
        <KPI label={t('kpi.node')} value={dur(k.nodeTimeMs)} unknownLabel={unknown} />
        <span className="kpi-with-tip">
          <KPI label={t('kpi.saved')} value={dur(k.savedMs)} unknownLabel={unknown} />
          <HelpTip>{t('kpi.savedHelp')}</HelpTip>
        </span>
        <KPI label={t('kpi.retries')} value={k.retries} unknownLabel={unknown} />
        <span className="kpi-with-tip">
          <KPI label={t('kpi.tokens')} unknownLabel={unknown} />
          <HelpTip>{t('kpi.tokensHelp')}</HelpTip>
        </span>
      </div>
      {run.status === 'running' && run.lastEventAt && <div className="mu small-print">{t('kpi.asOf', { time: time(locale, run.lastEventAt) })}</div>}
    </Card>
  )
}

function TimelineCard({ run }: { run: RunView }) {
  const { t } = useI18n()
  const tl = timeline(run)
  return (
    <Card title={t('run.timeline')} subtitle={tl && tl.bucketSeconds > 1 ? t('run.timelineBucket', { seconds: tl.bucketSeconds }) : undefined} action={<HelpTip>{t('run.timelineHelp')}</HelpTip>} testId="run-timeline">
      {tl && tl.rows.length > 0 ? (
        <DotTimeline rows={tl.rows} label={t('run.timeline')} caption={tl.bucketSeconds > 1 ? t('run.timelineBucket', { seconds: tl.bucketSeconds }) : undefined} />
      ) : (
        <div className="mu empty-line">{t('run.timelineEmpty')}</div>
      )}
    </Card>
  )
}

function EventsCard({ run }: { run: RunView }) {
  const { t, locale } = useI18n()
  const [filter, setFilter] = useState<EventFilter>('all')
  const shown = filterEvents(run.tail, filter)
  const rows: EventRow[] = shown.map((e) => ({
    sequence: e.sequence,
    time: time(locale, e.timestamp),
    type: e.type,
    node: e.node,
    detail: e.detail,
    tone: e.kind === 'failed' ? 'x' : e.kind === 'human' ? 'w' : e.kind === 'retried' ? 'b' : undefined,
  }))
  return (
    <Card
      title={t('run.eventsTitle')}
      subtitle={run.eventCount > run.tail.length ? t('events.showing', { count: run.tail.length, total: run.eventCount }) : undefined}
      action={<HelpTip>{t('run.eventsHelp')}</HelpTip>}
      testId="run-events"
    >
      <SegmentedControl
        label={t('events.filter')}
        value={filter}
        onChange={setFilter}
        segments={(['all', 'failed', 'retried', 'human'] as const).map((f) => ({ value: f, label: t(`events.${f}`), testId: `events-${f}` }))}
      />
      <EventLog rows={rows} emptyLabel={t('events.empty')} label={t('run.eventsTitle')} />
    </Card>
  )
}

export function RunInspector({ runId, onBack, onLost }: { runId: string; onBack: () => void; onLost?: (lost: boolean) => void }) {
  const { t } = useI18n()
  const { run, graph, yaml, following, notFound, lost, reconnect } = useRunStream(runId)
  const [selected, setSelected] = useState<string | null>(null)
  const [view, setView] = useState<GraphView>('map')
  const isLost = lost !== null
  useEffect(() => {
    onLost?.(isLost)
    return () => onLost?.(false)
  }, [isLost, onLost])
  if (notFound)
    return (
      <section>
        <Button onClick={onBack}>{t('run.back')}</Button>
        <div className="empty-state">{t('run.notFound')}</div>
      </section>
    )
  const waiting = run.order.filter((id) => displayStatus(nodeView(run, id)) === 'waiting_human')
  const focus = selected ?? waiting[0] ?? run.order.find((id) => nodeView(run, id).status === 'failed') ?? null
  const map = graph ? <GraphMap graph={graph} run={run} selected={focus} onSelect={setSelected} /> : <div className="empty-state">{t('run.loadingGraph')}</div>
  return (
    <section className="inspector">
      <div className="row between">
        <div className="row">
          <Button onClick={onBack}>{t('run.back')}</Button>
          <h1>{run.graphId ?? 'Run'}</h1>
          {lost ? <Pill tone="x" testId="run-status">{t('status.connectionLost')}</Pill> : <RunPill status={run.status} />}
          {following && run.status === 'running' && <span className="mu">{t('status.live')}</span>}
          {run.failureKind && <Tag>{run.failureKind}</Tag>}
        </div>
        <span className="mono mu" data-testid="event-count">{t('run.events', { count: run.eventCount })}</span>
      </div>
      {lost && (
        <div className="banner" role="alert" data-testid="connection-lost">
          <span>{t('run.lostBanner', { message: lost })}</span>
          <Button onClick={reconnect}>{t('run.reconnect')}</Button>
        </div>
      )}
      <RunSummaryCard run={run} />
      <div className="split">
        <div className="stack">
        <Card
          title={t('run.liveGraph')}
          testId="graph-card"
          action={
            <span className="row">
              <SegmentedControl
                label={t('run.view')}
                value={view}
                onChange={setView}
                segments={[
                  { value: 'map', label: t('run.view.map'), testId: 'view-map' },
                  { value: 'yaml', label: t('run.view.yaml'), testId: 'view-yaml' },
                  { value: 'split', label: t('run.view.split'), testId: 'view-split' },
                ]}
              />
              <HelpTip>{t('run.liveGraphHelp')}</HelpTip>
            </span>
          }
        >
          <div className={`graph-body view-${view}`}>
            {view !== 'yaml' && <div className="graph-map">{map}</div>}
            {view !== 'map' && <CodeView text={yaml} testId="graph-yaml" label={t('run.view.yaml')} />}
          </div>
        </Card>
        <TimelineCard run={run} />
        </div>
        <div className="stack">
          <NodePanel key={focus ?? ''} run={run} nodeId={focus} lost={lost !== null} kind={graph?.nodes.find((n) => n.id === focus)?.type} />
          <EventsCard run={run} />
        </div>
      </div>
    </section>
  )
}

export function Approvals({ onOpen }: { onOpen: (runId: string) => void }) {
  const { t } = useI18n()
  const approvals = usePoll(listApprovals, [], 1000)
  const [chosen, setChosen] = useState<string | null>(null)
  if (approvals.length === 0)
    return (
      <section>
        <h1>{t('approvals.title')}</h1>
        <div className="empty-state" data-testid="no-approvals">
          {t('approvals.empty')}
        </div>
      </section>
    )
  // Unless the person picked one that is still pending, show the first.
  const current = approvals.find((a) => a.node_run_id === chosen) ?? approvals[0]!
  return (
    <section>
      <h1>{t('approvals.title')}</h1>
      <div className="approvals-grid">
        <Card title={t('approvals.inbox')} subtitle={t('approvals.pendingCount', { count: approvals.length })} action={<HelpTip>{t('approvals.inboxHelp')}</HelpTip>} testId="approvals-inbox">
          {approvals.map((a) => (
            <ListRow key={a.node_run_id} selected={a.node_run_id === current.node_run_id} onClick={() => setChosen(a.node_run_id)} testId="approval-row">
              <Pill tone="w" small>{t('status.approval.pending')}</Pill>
              <span style={{ flex: 1 }}>{a.node_id}</span>
              <span className="mono mu">{a.run_id.slice(0, 8)}</span>
            </ListRow>
          ))}
        </Card>
        <Card
          key={current.node_run_id}
          title={current.node_id}
          subtitle={t('approvals.run', { id: current.run_id.slice(0, 8) })}
          action={<Button size="sm" onClick={() => onOpen(current.run_id)}>{t('approval.openRun')}</Button>}
          testId="approval-card"
        >
          <ApprovalBox runId={current.run_id} nodeRunId={current.node_run_id} prompt={current.prompt} context={current.context} />
        </Card>
      </div>
    </section>
  )
}

const SEVERITY_TONE: Record<string, 'x' | 'w' | 'n'> = { error: 'x', warning: 'w' }

export function RunConfig({ onStarted, initialPath }: { onStarted: (runId: string) => void; initialPath?: string }) {
  const { t } = useI18n()
  const [path, setPath] = useState(initialPath ?? '')
  const [inputs, setInputs] = useState('')
  const [result, setResult] = useState<{ status: string; diagnostics: Diagnostic[] } | null>(null)
  const [error, setError] = useState<ApiFailure | null>(null)
  const [busy, setBusy] = useState(false)

  async function validate(p = path) {
    setError(null)
    setResult(null)
    const r = await validateGraph(p)
    if ('message' in r) setError(r)
    else setResult(r)
  }
  async function choose() {
    const picked = await window.agentplane.pickGraph()
    if (picked) {
      setPath(picked)
      void validate(picked)
    }
  }
  async function run() {
    setBusy(true)
    setError(null)
    const lines = inputs.split('\n').map((l) => l.trim()).filter(Boolean)
    const r = await startRun(path, lines)
    setBusy(false)
    if ('message' in r) setError(r)
    else onStarted(r.runId)
  }
  // Opened with a graph already chosen (Dashboard "Open graph"): validate it right away.
  useEffect(() => {
    if (initialPath) void validate(initialPath)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialPath])
  const state = preflightState(result)
  const diagnostics = [...(result?.diagnostics ?? []), ...(error?.diagnostics ?? [])]
  const pill = state === 'ready' ? <Pill tone="p" testId="preflight-status">{t('config.ready')}</Pill> : state === 'warnings' ? <Pill tone="w" testId="preflight-status">{t('config.warnings')}</Pill> : state === 'blocked' ? <Pill tone="x" testId="preflight-status">{t('config.blocked')}</Pill> : null
  return (
    <section className="config">
      <h1>{t('config.title')}</h1>
      <Step n={1} title={t('config.step.graph')}>
        <Field label={t('config.graphFile')} htmlFor="graph-path">
          <div className="row">
            <input id="graph-path" data-testid="graph-path" value={path} placeholder="/path/to/graph.yaml" onChange={(e) => { setPath(e.target.value); setResult(null) }} />
            <Button data-testid="choose" onClick={choose}>{t('config.choose')}</Button>
            <Button data-testid="validate" disabled={!path} onClick={() => validate()}>{t('config.validate')}</Button>
          </div>
        </Field>
      </Step>
      <Step n={2} title={t('config.step.inputs')}>
        <Field label={t('config.inputs')} htmlFor="inputs">
          <textarea id="inputs" value={inputs} onChange={(e) => setInputs(e.target.value)} />
        </Field>
      </Step>
      <Step n={3} title={t('config.step.preflight')} action={<span className="row">{pill}<HelpTip>{t('config.preflightHelp')}</HelpTip></span>} testId="preflight-card">
        {state === 'idle' && diagnostics.length === 0 && !error && <div className="mu empty-line">{t('config.preflightIdle')}</div>}
        {state === 'ready' && diagnostics.length === 0 && (
          <div className="check-row">
            <Pill tone="p" small>{t('config.ready')}</Pill>
            <span>{t('config.graphValid')}</span>
          </div>
        )}
        {error && <div className="err empty-line">{error.message}</div>}
        {diagnostics.map((d, i) => (
          <div className="check-row diag" key={i} data-testid="diagnostic">
            <Pill tone={SEVERITY_TONE[d.severity] ?? 'n'} small>{d.severity === 'error' || d.severity === 'warning' ? t(`config.severity.${d.severity}`) : t('config.severity.info')}</Pill>
            <div className="check-text">
              <div>
                <b className="mono">{d.code}</b> {d.message}
                {d.location?.node_id && <> <Tag>{d.location.node_id}</Tag></>}
              </div>
              {d.suggestion && <div className="mu">{d.suggestion}</div>}
            </div>
          </div>
        ))}
      </Step>
      <div className="row">
        <Button variant="primary" data-testid="run" disabled={!canStart(state) || busy} onClick={run}>{t('config.run')}</Button>
      </div>
    </section>
  )
}
