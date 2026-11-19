import { useMemo } from 'react'
import type { CriticalPath } from './criticalPath'
import { formatDateTime, type Locale, type MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { agentTime, formatDuration, runKpis } from './insight'
import { filterCounts, filterUnavailable, NODE_FILTERS, type NodeFilter } from './nodeFilter'
import { outcomeSummary } from './outcome'
import type { RunView } from './reducer'
import type { GraphSummary } from './types'
import { Card, HelpTip, KPI, SegmentedControl, Tag } from './ui/primitives'
import { waitSummary } from './waits'

// The run overview of F2.6: figures, critical path, "what failed", identity and
// node filters. Each is a view of a pure function over the reduced run and the
// graph summary; none of them decides run state.

export const clockTime = (locale: Locale, iso: string) => new Intl.DateTimeFormat(locale, { timeStyle: 'medium' }).format(new Date(iso))

const VERSION_CHARS = 12

/** Run ID, start time and the first 12 characters of the graph version; each is unknown when absent. */
export function RunIdentity({ run }: { run: RunView }) {
  const { t, locale } = useI18n()
  const unknown = t('kpi.unknown')
  return (
    <div className="ov-line mu" data-testid="run-identity">
      <span>
        {t('run.id')}: <span className="mono ov-id" data-testid="run-id">{run.runId ?? unknown}</span>
      </span>
      <span>
        {t('run.startedAt')}: <span data-testid="run-started">{run.startedAt ? formatDateTime(locale, run.startedAt) : unknown}</span>
      </span>
      <span>
        {t('run.graphVersion')}: <span className="mono" data-testid="run-version">{run.graphVersion ? run.graphVersion.slice(0, VERSION_CHARS) : unknown}</span>
      </span>
    </div>
  )
}

export function RunSummaryCard({ run, graph, path, lost }: { run: RunView; graph: GraphSummary | null; path: CriticalPath | undefined; lost: boolean }) {
  const { t, locale } = useI18n()
  const k = runKpis(run)
  const waits = useMemo(() => waitSummary(run), [run])
  const agent = useMemo(() => agentTime(run, graph), [run, graph])
  const dur = (v: number | undefined) => (v === undefined ? undefined : formatDuration(v, locale))
  const unknown = t('kpi.unknown')
  const soFar = (is: boolean) => (is ? <Tag>{t('kpi.soFar')}</Tag> : null)
  return (
    <Card title={t('run.summary')} action={<HelpTip>{t('run.summaryHelp')}</HelpTip>} testId="run-summary">
      <div className="kpis">
        <KPI label={t('kpi.wall')} value={dur(k.wallClockMs)} unknownLabel={unknown} />
        <span className="kpi-with-tip">
          <KPI label={t('kpi.node')} value={dur(k.nodeTimeMs)} unknownLabel={unknown} />
          <HelpTip>{t('kpi.nodeHelp')}</HelpTip>
        </span>
        <span className="kpi-with-tip">
          <KPI label={t('kpi.saved')} value={dur(k.savedMs)} unknownLabel={unknown} />
          <HelpTip>{t('kpi.savedHelp')}</HelpTip>
        </span>
        <span className="kpi-with-tip" data-testid="kpi-agent">
          <KPI label={t('kpi.agent')} value={agent.kind === 'notApplicable' ? t('kpi.notApplicable') : agent.kind === 'value' ? formatDuration(agent.ms, locale) : undefined} unknownLabel={unknown} />
          <HelpTip>{t('kpi.agentHelp')}</HelpTip>
        </span>
        <span className="kpi-with-tip" data-testid="kpi-queued">
          <KPI label={t('kpi.queued')} value={dur(waits.queuedMs)} unknownLabel={unknown} />
          <HelpTip>{t('kpi.queuedHelp')}</HelpTip>
          {soFar(waits.queueSoFar)}
        </span>
        <span data-testid="kpi-human">
          <KPI label={t('kpi.humanWait')} value={waits.humanWaitMs === undefined ? t('kpi.notApplicable') : formatDuration(waits.humanWaitMs, locale)} unknownLabel={unknown} />
          {soFar(waits.humanSoFar)}
        </span>
        <KPI label={t('kpi.retries')} value={k.retries} unknownLabel={unknown} />
        <span className="kpi-with-tip">
          <KPI label={t('kpi.tokens')} unknownLabel={unknown} />
          <HelpTip>{t('kpi.tokensHelp')}</HelpTip>
        </span>
      </div>
      <div className="ov-grid">
        <div className="ov-line" data-testid="waited-longest">
          <b>{t('kpi.waitedLongest')}:</b>
          {waits.longest.length === 0
            ? t('overview.longestNone')
            : waits.longest.map((w) => (
                <span key={`${w.nodeId}:${w.kind}`}>
                  {t('overview.longestItem', { node: w.nodeId, kind: t(w.kind === 'queue' ? 'overview.waitQueue' : 'overview.waitHuman'), duration: formatDuration(w.ms, locale) })} {soFar(w.soFar)}
                </span>
              ))}
        </div>
        {waits.unknownNodes > 0 && (
          <div className="mu" data-testid="waits-unknown">
            {t('overview.unknownNodes', { count: waits.unknownNodes })}
          </div>
        )}
        <CriticalPathLine hasGraph={graph !== null} path={path} wallClockMs={k.wallClockMs} />
        {lost && run.lastEventAt && (
          <div className="mu" role="status" data-testid="overview-lost">
            {t('overview.lostNote', { time: clockTime(locale, run.lastEventAt) })}
          </div>
        )}
      </div>
      {run.status === 'running' && run.lastEventAt && <div className="mu small-print">{t('kpi.asOf', { time: clockTime(locale, run.lastEventAt) })}</div>}
    </Card>
  )
}

function CriticalPathLine({ hasGraph, path, wallClockMs }: { hasGraph: boolean; path: CriticalPath | undefined; wallClockMs: number | undefined }) {
  const { t, locale } = useI18n()
  const share = path && wallClockMs !== undefined && wallClockMs > 0 ? new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }).format(path.totalMs / wallClockMs) : undefined
  return (
    <div className="ov-line" data-testid="critical-path">
      <b>{t('overview.critical')}</b>
      <HelpTip>{t('overview.criticalHelp')}</HelpTip>
      {!hasGraph ? (
        <span className="mu">{t('overview.criticalNeedsGraph')}</span>
      ) : !path ? (
        <span className="mu">{t('overview.criticalUnknown')}</span>
      ) : (
        <>
          <span className="mono" data-testid="critical-path-nodes">{path.nodes.join(' → ')}</span>
          <span>{share ? t('overview.criticalShare', { duration: formatDuration(path.totalMs, locale), share }) : t('overview.criticalTotal', { duration: formatDuration(path.totalMs, locale) })}</span>
          {path.soFar && <Tag>{t('kpi.soFar')}</Tag>}
        </>
      )}
    </div>
  )
}

/** "What failed": absent for a succeeded run, and for a running one with nothing failed. */
export function OutcomeCard({ run, onOpen }: { run: RunView; onOpen: (nodeId: string) => void }) {
  const { t } = useI18n()
  const outcome = useMemo(() => outcomeSummary(run), [run])
  if (!outcome) return null
  const counts = [
    outcome.skipped > 0 && t('outcome.skipped', { count: outcome.skipped }),
    outcome.cancelled > 0 && t('outcome.cancelled', { count: outcome.cancelled }),
    outcome.interrupted > 0 && t('outcome.interrupted', { count: outcome.interrupted }),
  ].filter(Boolean)
  return (
    <Card title={t('outcome.title')} subtitle={outcome.soFar ? t('outcome.soFar') : undefined} action={<HelpTip>{t('outcome.help')}</HelpTip>} testId="run-outcome">
      {outcome.failed.length === 0 && <div className="mu empty-line">{t('outcome.noFailedNode')}</div>}
      {outcome.failed.map((f) => (
        <button key={f.nodeId} type="button" className="outcome-entry" onClick={() => onOpen(f.nodeId)} data-testid={`outcome-${f.nodeId}`}>
          <span className="row">
            <b>{f.nodeId}</b>
            {f.failureKind && <Tag>{f.failureKind}</Tag>}
            {f.diagnosticCodes.map((c) => (
              <Tag key={c}>{c}</Tag>
            ))}
          </span>
          {f.reason && <span className="mu">{t('outcome.reason', { reason: f.reason })}</span>}
          {f.decision && (
            <span className="mu" data-testid={`outcome-decision-${f.nodeId}`}>
              {t('outcome.rejected')}
              {f.decision.comment ? `: ${f.decision.comment}` : ''}
            </span>
          )}
        </button>
      ))}
      {counts.length > 0 && (
        <div className="mu empty-line" data-testid="outcome-counts">
          {counts.join(' · ')}
        </div>
      )}
    </Card>
  )
}

/** The node filter above the timeline; its options carry the number of nodes they select. */
export function NodeFilterBar({ run, graph, filter, onChange }: { run: RunView; graph: GraphSummary | null; filter: NodeFilter; onChange: (f: NodeFilter) => void }) {
  const { t } = useI18n()
  const counts = useMemo(() => filterCounts(run, graph), [run, graph])
  const reasons = NODE_FILTERS.flatMap((f) => {
    const why = filterUnavailable(run, graph, f)
    return why ? [t(why === 'graph' ? 'nodes.needsGraph' : 'nodes.slowNeedsSample')] : []
  })
  return (
    <div className="stack" data-testid="node-filter">
      <SegmentedControl
        label={t('nodes.filter')}
        value={filter}
        onChange={onChange}
        segments={NODE_FILTERS.map((f) => ({
          value: f,
          label: t('nodes.option', { label: t(`nodes.${f}` as MessageKey), count: counts[f] }),
          testId: `nodes-${f}`,
          disabled: filterUnavailable(run, graph, f) !== undefined,
        }))}
      />
      {reasons.map((r) => (
        <span key={r} className="mu small-print">
          {r}
        </span>
      ))}
    </div>
  )
}
