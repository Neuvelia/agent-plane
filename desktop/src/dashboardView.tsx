import { useState } from 'react'
import { listApprovals, listRuns } from './api'
import { activeRuns, ATTENTION_PREVIEW, needsAttention, outcomes } from './dashboard'
import { useConnectionReport } from './connection'
import { usePoll } from './hooks'
import { EngineErrorState, LoadingState } from './states'
import { formatDateTime } from './i18n'
import { useI18n } from './i18n/context'
import { formatDuration } from './insight'
import { RunPill } from './status'
import { DotChart } from './ui/data'
import { Button, Card, HelpTip, ListRow, Pill } from './ui/primitives'

/** `nowOverride` exists so stories and tests are deterministic; the app passes nothing. */
export function Dashboard({ onOpenRun, onNewRun, onOpenGraph, onAllRuns, nowOverride }: { onOpenRun: (id: string) => void; onNewRun: () => void; onOpenGraph: (path: string) => void; onAllRuns: () => void; nowOverride?: Date }) {
  const { t, locale } = useI18n()
  const polledRuns = usePoll(listRuns, 1500)
  const polledApprovals = usePoll(listApprovals, 1000)
  const failed = polledRuns.status === 'error' || polledApprovals.status === 'error'
  useConnectionReport('dashboard', failed)
  const runs = polledRuns.data ?? []
  const approvals = polledApprovals.data ?? []
  const [expanded, setExpanded] = useState(false)
  const now = nowOverride ?? new Date()
  const items = needsAttention(runs, approvals)
  const shown = expanded ? items : items.slice(0, ATTENTION_PREVIEW)
  const active = activeRuns(runs)
  const { dots, slots } = outcomes(runs, now)

  async function openGraph() {
    const picked = await window.agentplane.pickGraph()
    if (picked) onOpenGraph(picked)
  }

  if (failed || !polledRuns.data || !polledApprovals.data)
    return (
      <section className="dashboard" data-testid="dashboard">
        <h1>{t('dash.title')}</h1>
        {failed ? <EngineErrorState onRetry={() => { polledRuns.retry(); polledApprovals.retry() }} /> : <LoadingState />}
      </section>
    )
  return (
    <section className="dashboard" data-testid="dashboard">
      <h1>{t('dash.title')}</h1>
      <div className="dash-grid">
        <div className="stack">
          <Card title={t('dash.attention')} subtitle={t('dash.attentionSub', { count: items.length })} action={<HelpTip>{t('dash.attentionHelp')}</HelpTip>} testId="needs-attention">
            {items.length === 0 && <div className="mu empty-line">{t('dash.attentionEmpty')}</div>}
            {shown.map((it) =>
              it.kind === 'approval' ? (
                <ListRow key={it.approval.node_run_id} onClick={() => onOpenRun(it.approval.run_id)} testId="attention-item">
                  <Pill tone="w" small>{t('dash.approval')}</Pill>
                  <span style={{ flex: 1 }}>{it.approval.node_id}</span>
                  <span className="mono mu">{it.approval.run_id.slice(0, 8)}</span>
                </ListRow>
              ) : (
                <ListRow key={it.run.run_id} onClick={() => onOpenRun(it.run.run_id)} testId="attention-item">
                  <RunPill status={it.run.status} />
                  <span style={{ flex: 1 }}>{it.run.graph_id}</span>
                  <span className="mono mu">{it.run.run_id.slice(0, 8)}</span>
                </ListRow>
              ),
            )}
            {items.length > ATTENTION_PREVIEW && (
              <div className="more">
                <Button size="sm" onClick={() => setExpanded((v) => !v)} data-testid="attention-more">
                  {expanded ? t('dash.showLess') : t('dash.showMore')}
                </Button>
              </div>
            )}
          </Card>
          <Card title={t('dash.active')} subtitle={t('dash.activeSub', { count: active.length })} action={<Button size="sm" onClick={onAllRuns}>{t('dash.allRuns')}</Button>} testId="active-runs">
            {active.length === 0 && <div className="mu empty-line">{t('dash.activeEmpty')}</div>}
            {active.map((r) => (
              <ListRow key={r.run_id} onClick={() => onOpenRun(r.run_id)} testId="active-run">
                <RunPill status={r.status} />
                <span style={{ flex: 1 }}>{r.graph_id}</span>
                <span className="mono mu">{formatDuration(Math.max(0, now.getTime() - Date.parse(r.started_at)), locale)}</span>
                <span className="mono mu">{r.run_id.slice(0, 8)}</span>
              </ListRow>
            ))}
          </Card>
        </div>
        <div className="stack">
          <Card title={t('dash.outcomes')} subtitle={t('dash.outcomesSub', { count: dots.length })} testId="outcomes">
            <DotChart
              label={t('dash.outcomesLabel')}
              slots={slots}
              dots={dots.map((d) => ({ key: d.run.run_id, tone: d.tone, label: `${d.run.graph_id} · ${formatDateTime(locale, d.run.started_at)}` }))}
            />
            <div className="legend">
              <span className="legend-item"><span className="dot sm d-p" aria-hidden="true" />{t('dash.legend.succeeded')}</span>
              <span className="legend-item"><span className="dot sm d-x" aria-hidden="true" />{t('dash.legend.failed')}</span>
              <span className="legend-item"><span className="dot sm d-b" aria-hidden="true" />{t('dash.legend.running')}</span>
              <span className="legend-item"><span className="dot sm d-n" aria-hidden="true" />{t('dash.legend.other')}</span>
            </div>
          </Card>
          <Card title={t('dash.quick')} testId="quick-actions">
            <ListRow onClick={onNewRun} testId="quick-new">
              <span style={{ flex: 1 }}>
                <b>{t('dash.quick.new')}</b>
                <div className="mu">{t('dash.quick.newHint')}</div>
              </span>
            </ListRow>
            <ListRow onClick={() => void openGraph()} testId="quick-open">
              <span style={{ flex: 1 }}>
                <b>{t('dash.quick.open')}</b>
                <div className="mu">{t('dash.quick.openHint')}</div>
              </span>
            </ListRow>
          </Card>
        </div>
      </div>
    </section>
  )
}
