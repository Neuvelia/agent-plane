import { useState } from 'react'
import { listApprovals, listRuns } from './api'
import { useConnectionReport } from './connection'
import { usePoll } from './hooks'
import { ageMs, filterQueue, formatAge, inboxQueue, nextItem, queueCounts, recentOutcomes, selectItem, withDecided, type InboxFilter, type InboxGroup, type InboxItem } from './inbox'
import { InboxDetail, KIND_GLYPH } from './InboxDetail'
import type { MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { EngineErrorState, LoadingState } from './states'
import type { ChosenGraph, RunSummary } from './types'
import { Glyph } from './ui/glyphs'

// F2.5b: the Inbox is a triage queue (Tarmind Dashboard UX canvas, artboard
// "Inbox: triage queue"). The queue is on the left, the selected item with its
// evidence on the right; deciding an approval never leaves the screen.

const GROUPS: InboxGroup[] = ['waiting', 'stopped', 'running']
const FILTERS: InboxFilter[] = ['all', 'approvals', 'stopped', 'running']

/** "14 min" or "just now"; the empty string when the time is unknown. */
export function useAge(now: Date): (iso: string) => string {
  const { t, locale } = useI18n()
  return (iso) => {
    const ms = ageMs(iso, now)
    if (ms === undefined) return ''
    return formatAge(ms, locale) ?? t('inbox.justNow')
  }
}

function RecentRuns({ runs }: { runs: RunSummary[] }) {
  const { t } = useI18n()
  const recent = recentOutcomes(runs)
  if (recent.length === 0) return null
  const count = (tone: string) => recent.filter((o) => o.tone === tone).length
  return (
    <div className="inbox-recent">
      <span className="spark" role="img" aria-label={t('inbox.recentLabel', { count: recent.length, ok: count('p'), failed: count('x'), running: count('b') })} data-testid="inbox-recent">
        {recent.map((o) => (
          <i key={o.run.run_id} className={`s-${o.tone}`} />
        ))}
      </span>
      <span className="mu">{t('inbox.recent', { ok: count('p'), count: recent.length })}</span>
    </div>
  )
}

function Filters({ value, counts, onChange }: { value: InboxFilter; counts: Record<InboxFilter, number>; onChange: (f: InboxFilter) => void }) {
  const { t } = useI18n()
  return (
    <div className="inbox-tabs" role="group" aria-label={t('inbox.filter')}>
      {FILTERS.map((f) => (
        <button key={f} type="button" aria-pressed={f === value} onClick={() => onChange(f)} data-testid={`inbox-filter-${f}`}>
          {t(`inbox.filter.${f}`)}
          <span className="mono">{counts[f]}</span>
        </button>
      ))}
    </div>
  )
}

function QueueRow({ item, selected, age, decision, onPick }: { item: InboxItem; selected: boolean; age: string; decision?: string; onPick: () => void }) {
  const { t } = useI18n()
  const run = item.runId.slice(0, 8)
  const sub = item.kind === 'approval' ? t('inbox.sub.approval', { node: item.approval?.node_id ?? '', run }) : t(`inbox.sub.${item.kind}` as MessageKey, { run })
  const cls = ['inbox-row', selected ? 'sel' : '', decision ? 'done' : ''].filter(Boolean).join(' ')
  return (
    <li>
      <button type="button" className={cls} aria-current={selected ? 'true' : undefined} onClick={onPick} data-testid="inbox-item" data-kind={item.kind}>
        <span className="inbox-row-glyph"><Glyph kind={KIND_GLYPH[item.kind]} /></span>
        <span className="inbox-row-text">
          <span className="inbox-row-name">{item.graphId}</span>
          <span className="inbox-row-sub">{sub}</span>
        </span>
        {decision ? (
          <span className="inbox-row-age decided">{t(decision === 'approved' ? 'status.approval.approved' : 'status.approval.rejected')}</span>
        ) : (
          <span className="inbox-row-age mono">{age}</span>
        )}
      </button>
    </li>
  )
}

function InboxEmpty({ onNewRun, onOpenGraph, onAllRuns }: { onNewRun: () => void; onOpenGraph: (graph: ChosenGraph) => void; onAllRuns: () => void }) {
  const { t } = useI18n()
  const [openError, setOpenError] = useState<string | null>(null)
  async function openGraph() {
    setOpenError(null)
    const picked = await window.agentplane.openGraph()
    if ('canceled' in picked) return
    if (picked.ok) onOpenGraph({ grant: picked.grant, label: picked.label })
    else setOpenError(picked.message)
  }
  return (
    <div className="inbox-empty" data-testid="inbox-empty">
      <Glyph kind="ok" />
      <h2>{t('inbox.empty.title')}</h2>
      <p className="mu">{t('inbox.empty.body')}</p>
      <div className="inbox-empty-actions">
        <button type="button" className="ib-btn pr" onClick={onNewRun} data-testid="quick-new">{t('inbox.quick.new')}</button>
        <button type="button" className="ib-btn" onClick={() => void openGraph()} data-testid="quick-open">{t('inbox.quick.open')}</button>
        <button type="button" className="ib-btn ghost" onClick={onAllRuns} data-testid="inbox-all-runs">{t('inbox.allRuns')}</button>
      </div>
      {openError && <div className="err" role="alert" data-testid="open-error">{openError}</div>}
    </div>
  )
}

export interface InboxProps {
  onOpenRun: (id: string) => void
  onNewRun: () => void
  onOpenGraph: (graph: ChosenGraph) => void
  onAllRuns: () => void
  /** Pins "now" so stories and tests are deterministic; the app passes nothing. */
  nowOverride?: Date
}

/** Approvals decided here, with the decision their run's events recorded; view state only. */
type Decided = Record<string, { item: InboxItem; decision?: string }>

export function Inbox({ onOpenRun, onNewRun, onOpenGraph, onAllRuns, nowOverride }: InboxProps) {
  const { t, locale } = useI18n()
  const polledRuns = usePoll(listRuns, 1500)
  const polledApprovals = usePoll(listApprovals, 1000)
  const failed = polledRuns.status === 'error' || polledApprovals.status === 'error'
  useConnectionReport('inbox', failed)
  const [filter, setFilter] = useState<InboxFilter>('all')
  const [picked, setPicked] = useState<string | null>(null)
  // A decided approval stays in the queue, dimmed, with the decision its events recorded (as on the canvas).
  const [decided, setDecided] = useState<Decided>({})
  const now = nowOverride ?? new Date()
  const age = useAge(now)
  const date = new Intl.DateTimeFormat(locale, { weekday: 'long', day: 'numeric', month: 'long' }).format(now)

  if (failed || !polledRuns.data || !polledApprovals.data)
    return (
      <section className="inbox-state" data-testid="inbox">
        <h1>{t('inbox.title')}</h1>
        {failed ? <EngineErrorState onRetry={() => { polledRuns.retry(); polledApprovals.retry() }} /> : <LoadingState />}
      </section>
    )
  const live = inboxQueue(polledRuns.data, polledApprovals.data)
  const queue = withDecided(live, Object.values(decided).map((d) => d.item))
  const counts = queueCounts(live)
  const visible = filterQueue(queue, filter)
  const current = selectItem(visible, picked)
  const remember = (item: InboxItem, decision?: string) => setDecided((d) => ({ ...d, [item.key]: { item, decision: decision ?? d[item.key]?.decision } }))
  const next = () => setPicked(nextItem(live, current?.key ?? '')?.key ?? null)
  return (
    <section className="inbox" data-testid="inbox" aria-labelledby="inbox-title">
      <div className="inbox-queue" role="region" aria-label={t('inbox.queue')}>
        <header className="inbox-head">
          <div>
            <h1 id="inbox-title">{t('inbox.title')}</h1>
            <div className="mu inbox-date">{date}</div>
          </div>
          <RecentRuns runs={polledRuns.data} />
          <Filters value={filter} counts={counts} onChange={setFilter} />
        </header>
        <div className="inbox-list">
          {queue.length > 0 && visible.length === 0 && <p className="mu inbox-none">{t('inbox.filterEmpty')}</p>}
          {GROUPS.map((g) => {
            const rows = visible.filter((i) => i.group === g)
            if (rows.length === 0) return null
            return (
              <div key={g} className="inbox-group">
                <h2 className="inbox-label" id={`inbox-group-${g}`}>{t(`inbox.group.${g}`)}</h2>
                <ul aria-labelledby={`inbox-group-${g}`}>
                  {rows.map((i) => <QueueRow key={i.key} item={i} selected={i.key === current?.key} age={age(i.at)} decision={decided[i.key]?.decision} onPick={() => setPicked(i.key)} />)}
                </ul>
              </div>
            )
          })}
        </div>
      </div>
      <div className="inbox-detail" role="region" aria-label={t('inbox.selected')}>
        {current ? (
          <InboxDetail key={current.key} item={current} age={age(current.at)} onOpenRun={onOpenRun} onDecided={() => remember(current)} onSettled={(d) => remember(current, d)} onNext={next} />
        ) : (
          <InboxEmpty onNewRun={onNewRun} onOpenGraph={onOpenGraph} onAllRuns={onAllRuns} />
        )}
      </div>
    </section>
  )
}
