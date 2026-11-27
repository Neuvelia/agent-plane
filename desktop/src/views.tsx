import { useEffect, useMemo, useState } from 'react'
import { listApprovals, listRuns } from './api'
import { useConnectionReport } from './connection'
import { EngineErrorState, LoadingState } from './states'
import { GraphMap } from './GraphMap'
import { useRunStream, usePoll } from './hooks'
import { formatDateTime } from './i18n'
import { useI18n } from './i18n/context'
import { ApprovalBox, NodePanel } from './NodePanel'
import { nodeView, displayStatus, type RunView } from './reducer'
import { criticalPath } from './criticalPath'
import { filterEvents, formatDuration, timeline, type EventFilter } from './insight'
import { filterRuns, groupRuns, NO_REPO, repoLabels, repoOf, runDurationMs, shortVersion, sortedUnique, sourceLabel, type RunFilter, type RunGroupBy } from './runsFilter'
import { matchingNodes, type NodeFilter } from './nodeFilter'
import { clockTime, NodeFilterBar, OutcomeCard, RunIdentity, RunSummaryCard } from './overview'
import { NodeTimeline } from './timelineView'
import { RunPill } from './status'
import type { GraphSummary } from './types'
import { CodeView, EventLog, type EventRow } from './ui/data'
import { Button, Card, Field, HelpTip, ListRow, Pill, SegmentedControl, Tag } from './ui/primitives'

// R1: Run Inspector and Approvals on the design system (New run is NewRun.tsx, F2.5c). Behaviour and
// data are unchanged from F2.5; the Dashboard is redesigned in R2.

export function RunsList({ onOpen }: { onOpen: (id: string) => void }) {
  const { t, locale } = useI18n()
  const { data: runs, status, retry } = usePoll(listRuns, 1500)
  const [filter, setFilter] = useState<RunFilter>({ repo: '', graph: '' })
  const [groupBy, setGroupBy] = useState<RunGroupBy>('none')
  useConnectionReport('runs', status === 'error')
  const shown = useMemo(() => filterRuns(runs ?? [], filter), [runs, filter])
  const labels = useMemo(() => repoLabels(runs ?? []), [runs])
  const repoLabel = (key: string) => (key === NO_REPO ? t('runs.noRepo') : (labels.get(key) ?? key))
  const repos = useMemo(() => [...new Set((runs ?? []).map(repoOf))].sort((a, b) => repoLabel(a).localeCompare(repoLabel(b))), [runs, labels])
  const graphs = useMemo(() => sortedUnique((runs ?? []).map((r) => r.graph_id)), [runs])
  const groupLabel = (key: string) => (groupBy === 'repo' ? repoLabel(key) : key)
  return (
    <section>
      <h1>{t('runs.title')}</h1>
      {status === 'error' ? (
        <EngineErrorState onRetry={retry} />
      ) : !runs ? (
        <LoadingState />
      ) : runs.length === 0 ? (
        <div className="empty-state" data-testid="no-runs">
          {t('runs.empty')}
        </div>
      ) : (
        <>
          <div className="runs-filters" data-testid="runs-filters">
            <Field label={t('runs.filter.repo')} htmlFor="runs-filter-repo">
              <select id="runs-filter-repo" className="input" value={filter.repo} onChange={(e) => setFilter({ ...filter, repo: e.target.value })} data-testid="runs-filter-repo">
                <option value="">{t('runs.filter.all')}</option>
                {repos.map((r) => (
                  <option key={r} value={r}>
                    {repoLabel(r)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={t('runs.filter.graph')} htmlFor="runs-filter-graph">
              <select id="runs-filter-graph" className="input" value={filter.graph} onChange={(e) => setFilter({ ...filter, graph: e.target.value })} data-testid="runs-filter-graph">
                <option value="">{t('runs.filter.all')}</option>
                {graphs.map((g) => (
                  <option key={g} value={g}>
                    {g}
                  </option>
                ))}
              </select>
            </Field>
            <SegmentedControl
              label={t('runs.groupBy')}
              value={groupBy}
              onChange={setGroupBy}
              segments={(['none', 'repo', 'graph'] as const).map((g) => ({ value: g, label: t(`runs.group.${g}`), testId: `runs-group-${g}` }))}
            />
          </div>
          {shown.length === 0 ? (
            <div className="empty-state" data-testid="runs-no-match">
              {t('runs.noMatch')}
            </div>
          ) : (
            <table className="runs" data-testid="runs-table">
              <thead>
                <tr>
                  <th>{t('runs.col.status')}</th>
                  <th>{t('runs.col.graph')}</th>
                  <th>{t('runs.col.version')}</th>
                  <th>{t('runs.col.source')}</th>
                  <th>{t('runs.col.started')}</th>
                  <th>{t('runs.col.duration')}</th>
                  <th>{t('runs.col.events')}</th>
                  <th>{t('runs.col.run')}</th>
                </tr>
              </thead>
              {groupRuns(shown, groupBy).map((g) => (
                <tbody key={g.key} data-testid={groupBy === 'none' ? undefined : 'run-group'}>
                  {groupBy !== 'none' && (
                    <tr className="run-group-head">
                      <th colSpan={8} scope="colgroup">
                        {groupLabel(g.key)} <span className="mu mono">({g.runs.length})</span>
                      </th>
                    </tr>
                  )}
                  {g.runs.map((r) => {
                    const ms = runDurationMs(r)
                    const source = sourceLabel(r)
                    return (
                      <tr key={r.run_id} data-testid="run-row">
                        <td>
                          <RunPill status={r.status} />
                        </td>
                        {/* A real button; its ::after stretches over the row so the whole row is clickable. */}
                        <td>
                          <button type="button" className="row-link" onClick={() => onOpen(r.run_id)} data-testid="run-open">
                            {r.graph_id}
                          </button>
                        </td>
                        <td className="mono mu" title={r.graph_version} data-testid="run-graph-version">
                          {shortVersion(r.graph_version) ?? '—'}
                        </td>
                        <td className="mono" title={r.runtime_version ? `${r.repo_commit ?? ''} · v${r.runtime_version}` : r.repo_commit} data-testid="run-source">
                          {source ?? '—'}
                        </td>
                        <td className="mono">{formatDateTime(locale, r.started_at)}</td>
                        <td className="mono" data-testid="run-duration">
                          {ms === undefined ? '—' : formatDuration(ms, locale)}
                        </td>
                        <td className="mono">{r.event_count}</td>
                        <td className="mono mu">{r.run_id.slice(0, 8)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              ))}
            </table>
          )}
        </>
      )}
    </section>
  )
}

export type GraphView = 'map' | 'yaml' | 'split'

function TimelineCard({ run, graph, filter, onFilter, hidden, selected, critical, onSelect }: { run: RunView; graph: GraphSummary | null; filter: NodeFilter; onFilter: (f: NodeFilter) => void; hidden: ReadonlySet<string>; selected: string | null; critical: ReadonlySet<string>; onSelect: (id: string) => void }) {
  const { t } = useI18n()
  const tl = timeline(run)
  const caption = tl && tl.bucketSeconds > 1 ? t('run.timelineBucket', { seconds: tl.bucketSeconds }) : undefined
  return (
    <Card title={t('run.timeline')} subtitle={caption} action={<HelpTip>{t('run.timelineHelp')}</HelpTip>} testId="run-timeline">
      <NodeFilterBar run={run} graph={graph} filter={filter} onChange={onFilter} />
      {tl && tl.rows.length > 0 ? (
        <NodeTimeline timeline={tl} selected={selected} critical={critical} hidden={hidden} onSelect={onSelect} />
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
    time: clockTime(locale, e.timestamp),
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

export function RunInspector({ runId, onBack }: { runId: string; onBack: () => void }) {
  const { t } = useI18n()
  const { run, graph, yaml, following, notFound, unsupported, lost, reconnect } = useRunStream(runId)
  const [selected, setSelected] = useState<string | null>(null)
  const [view, setView] = useState<GraphView>('map')
  const [nodeFilter, setNodeFilter] = useState<NodeFilter>('all')
  const path = useMemo(() => (graph ? criticalPath(run, graph) : undefined), [run, graph])
  const critical = useMemo<ReadonlySet<string>>(() => new Set(path?.nodes ?? []), [path])
  // Nodes the filter leaves out: dimmed on the map and hidden from the timeline. View state only.
  const dimmed = useMemo<ReadonlySet<string>>(() => {
    if (nodeFilter === 'all') return new Set<string>()
    const keep = new Set(matchingNodes(run, graph, nodeFilter))
    return new Set((graph ? graph.nodes.map((n) => n.id) : run.order).filter((id) => !keep.has(id)))
  }, [run, graph, nodeFilter])
  const isLost = lost !== null
  useConnectionReport('run', isLost)
  if (notFound || unsupported)
    return (
      <section>
        <Button onClick={onBack}>{t('run.back')}</Button>
        <div className="empty-state" data-testid={unsupported ? 'run-unsupported' : 'run-not-found'}>
          {t(unsupported ? 'run.unsupported' : 'run.notFound')}
        </div>
      </section>
    )
  const waiting = run.order.filter((id) => displayStatus(nodeView(run, id)) === 'waiting_human')
  const focus = selected ?? waiting[0] ?? run.order.find((id) => nodeView(run, id).status === 'failed') ?? null
  // The node that is open is never dimmed, whatever the filter says.
  const mapDimmed = focus && dimmed.has(focus) ? new Set([...dimmed].filter((id) => id !== focus)) : dimmed
  const map = graph ? <GraphMap graph={graph} run={run} selected={focus} onSelect={setSelected} critical={critical} dimmed={mapDimmed} /> : <div className="empty-state">{t('run.loadingGraph')}</div>
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
          <span>{t('run.lostBanner', { message: t(lost === 'storage' ? 'run.lost.storage' : 'run.lost.connection') })}</span>
          <Button onClick={reconnect}>{t('run.reconnect')}</Button>
        </div>
      )}
      <RunIdentity run={run} />
      <RunSummaryCard run={run} graph={graph} path={path} lost={lost !== null} />
      <OutcomeCard run={run} onOpen={setSelected} />
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
        <TimelineCard run={run} graph={graph} filter={nodeFilter} onFilter={setNodeFilter} hidden={dimmed} selected={focus} critical={critical} onSelect={setSelected} />
        </div>
        <div className="stack">
          <NodePanel key={focus ?? ''} run={run} nodeId={focus} lost={lost !== null} kind={graph?.nodes.find((n) => n.id === focus)?.type} upstream={graph?.edges.filter((e) => e.to === focus).map((e) => e.from)} onOpenNode={setSelected} />
          <EventsCard run={run} />
        </div>
      </div>
    </section>
  )
}

export function Approvals({ onOpen }: { onOpen: (runId: string) => void }) {
  const { t } = useI18n()
  const { data: approvals, status, retry } = usePoll(listApprovals, 1000)
  const [chosen, setChosen] = useState<string | null>(null)
  useConnectionReport('approvals', status === 'error')
  if (status === 'error' || !approvals || approvals.length === 0)
    return (
      <section>
        <h1>{t('approvals.title')}</h1>
        {status === 'error' ? (
          <EngineErrorState onRetry={retry} />
        ) : !approvals ? (
          <LoadingState />
        ) : (
          <div className="empty-state" data-testid="no-approvals">
            {t('approvals.empty')}
          </div>
        )}
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
