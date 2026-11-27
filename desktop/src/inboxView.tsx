import { useState } from 'react'
import { listApprovals, listRuns } from './api'
import { useConnectionReport } from './connection'
import { usePoll } from './hooks'
import { ageMs, filterQueue, formatAge, inboxQueue, nextItem, queueCounts, recentOutcomes, selectItem, type InboxFilter, type InboxGroup, type InboxItem } from './inbox'
import { InboxDetail, KIND_GLYPH } from './InboxDetail'
import type { MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { EngineErrorState, LoadingState } from './states'
import type { ChosenGraph, RunSummary } from './types'
import { Glyph } from './ui/glyphs'
import { Button, SegmentedControl } from './ui/primitives'

// F2.5b: the Inbox is a triage queue. The queue is on the left, the selected
// item with its evidence on the right; deciding an approval never leaves the screen.

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

function QueueRow({ item, selected, age, onPick }: { item: InboxItem; selected: boolean; age: string; onPick: () => void }) {
  const { t } = useI18n()
  const run = item.runId.slice(0, 8)
  const sub = item.kind === 'approval' ? t('inbox.sub.approval', { node: item.approval?.node_id ?? '', run }) : t(`inbox.sub.${item.kind}` as MessageKey, { run })
  return (
    <li>
      <button type="button" className={selected ? 'inbox-row sel' : 'inbox-row'} aria-current={selected ? 'true' : undefined} onClick={onPick} data-testid="inbox-item" data-kind={item.kind}>
        <Glyph kind={KIND_GLYPH[item.kind]} />
        <span className="inbox-row-text">
          <span className="inbox-row-name">{item.graphId}</span>
          <span className="inbox-row-sub mono">{sub}</span>
        </span>
        <span className="inbox-row-age mono">{age}</span>
      </button>
    </li>
  )
}

function QuickActions({ onNewRun, onOpenGraph }: { onNewRun: () => void; onOpenGraph: (graph: ChosenGraph) => void }) {
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
    <div className="inbox-actions">
      <div className="row">
        <Button size="sm" onClick={onNewRun} data-testid="quick-new">{t('inbox.quick.new')}</Button>
        <Button size="sm" onClick={() => void openGraph()} data-testid="quick-open">{t('inbox.quick.open')}</Button>
      </div>
      {openError && <div className="err" role="alert" data-testid="open-error">{openError}</div>}
    </div>
  )
}

function InboxEmpty({ onAllRuns }: { onAllRuns: () => void }) {
  const { t } = useI18n()
  return (
    <div className="inbox-empty" data-testid="inbox-empty">
      <Glyph kind="ok" />
      <h2>{t('inbox.empty.title')}</h2>
      <p className="mu">{t('inbox.empty.body')}</p>
      <Button onClick={onAllRuns} data-testid="inbox-all-runs">{t('inbox.allRuns')}</Button>
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

export function Inbox({ onOpenRun, onNewRun, onOpenGraph, onAllRuns, nowOverride }: InboxProps) {
  const { t, locale } = useI18n()
  const polledRuns = usePoll(listRuns, 1500)
  const polledApprovals = usePoll(listApprovals, 1000)
  const failed = polledRuns.status === 'error' || polledApprovals.status === 'error'
  useConnectionReport('inbox', failed)
  const [filter, setFilter] = useState<InboxFilter>('all')
  const [picked, setPicked] = useState<string | null>(null)
  // An approval decided here stays open (with its decision) after it leaves the list, until Next or another pick.
  const [held, setHeld] = useState<InboxItem | null>(null)
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
  const queue = inboxQueue(polledRuns.data, polledApprovals.data)
  const counts = queueCounts(queue)
  const visible = filterQueue(queue, filter)
  const current = held ?? selectItem(visible, picked)
  const pick = (key: string) => {
    setHeld(null)
    setPicked(key)
  }
  const next = () => {
    const after = nextItem(queue, current?.key ?? '')
    setHeld(null)
    setPicked(after?.key ?? null)
  }
  return (
    <section className="inbox" data-testid="inbox" aria-labelledby="inbox-title">
      <div className="inbox-queue" role="region" aria-label={t('inbox.queue')}>
        <header className="inbox-head">
          <div>
            <h1 id="inbox-title">{t('inbox.title')}</h1>
            <div className="mu inbox-date">{date}</div>
          </div>
          <RecentRuns runs={polledRuns.data} />
          <SegmentedControl label={t('inbox.filter')} value={filter} onChange={setFilter} segments={FILTERS.map((f) => ({ value: f, label: `${t(`inbox.filter.${f}`)} ${counts[f]}`, testId: `inbox-filter-${f}` }))} />
        </header>
        <div className="inbox-list">
          {queue.length > 0 && visible.length === 0 && <p className="mu inbox-none">{t('inbox.filterEmpty')}</p>}
          {GROUPS.map((g) => {
            const rows = visible.filter((i) => i.group === g)
            if (rows.length === 0) return null
            return (
              <div key={g} className="inbox-group">
                <h2 className="lb" id={`inbox-group-${g}`}>{t(`inbox.group.${g}`)}</h2>
                <ul aria-labelledby={`inbox-group-${g}`}>
                  {rows.map((i) => <QueueRow key={i.key} item={i} selected={i.key === current?.key} age={age(i.at)} onPick={() => pick(i.key)} />)}
                </ul>
              </div>
            )
          })}
        </div>
        <QuickActions onNewRun={onNewRun} onOpenGraph={onOpenGraph} />
      </div>
      <div className="inbox-detail" role="region" aria-label={t('inbox.selected')}>
        {current ? (
          <InboxDetail key={current.key} item={current} age={age(current.at)} onOpenRun={onOpenRun} onDecided={() => setHeld(current)} onNext={next} />
        ) : (
          <InboxEmpty onAllRuns={onAllRuns} />
        )}
      </div>
    </section>
  )
}
