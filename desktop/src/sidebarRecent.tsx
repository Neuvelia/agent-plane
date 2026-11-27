import { ageMs, formatAge, newestRuns, SIDEBAR_RECENT } from './inbox'
import type { MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import type { PendingApproval, RunSummary } from './types'
import { Glyph, type GlyphKind } from './ui/glyphs'

// F2.5b: "Recent" in the sidebar, the newest runs from the run list (GET /runs).
// A failed read says so; it never shows an empty or stale list as current.

const RUN_GLYPH: Record<string, GlyphKind> = { running: 'running', succeeded: 'ok', failed: 'bad', interrupted: 'paused' }
const RUN_STATUS: Record<string, MessageKey> = {
  running: 'status.run.running',
  succeeded: 'status.run.succeeded',
  failed: 'status.run.failed',
  cancelled: 'status.run.cancelled',
  interrupted: 'status.run.interrupted',
}

export interface RecentRunsProps {
  runs: RunSummary[] | undefined
  approvals: PendingApproval[]
  failed: boolean
  onOpen: (runId: string) => void
  onAll: () => void
  /** Pins "now" so stories are deterministic; the app passes nothing. */
  nowOverride?: Date
}

function RecentRow({ run, waiting, now, onOpen }: { run: RunSummary; waiting: boolean; now: Date; onOpen: () => void }) {
  const { t, locale } = useI18n()
  const ms = ageMs(run.started_at, now)
  const age = ms === undefined ? '' : (formatAge(ms, locale, 'narrow') ?? t('inbox.justNow'))
  const status = waiting ? t('status.approval.pending') : RUN_STATUS[run.status] ? t(RUN_STATUS[run.status]!) : run.status
  return (
    <button type="button" className="side-run" onClick={onOpen} data-testid="recent-run">
      <Glyph kind={waiting ? 'approval' : (RUN_GLYPH[run.status] ?? 'idle')} />
      <span className="side-run-name">{run.graph_id}</span>
      <span className="sr-only">{status}</span>
      <span className="side-run-age mono">{age}</span>
    </button>
  )
}

export function RecentRuns({ runs, approvals, failed, onOpen, onAll, nowOverride }: RecentRunsProps) {
  const { t } = useI18n()
  const now = nowOverride ?? new Date()
  const waiting = new Set(approvals.map((a) => a.run_id))
  return (
    <div className="side-recent" role="group" aria-labelledby="side-recent">
      <div className="side-sec" id="side-recent">{t('nav.recent', { count: SIDEBAR_RECENT })}</div>
      {failed ? (
        <div className="side-note">{t('nav.recentUnavailable')}</div>
      ) : !runs ? null : runs.length === 0 ? (
        <div className="side-note">{t('nav.noRuns')}</div>
      ) : (
        newestRuns(runs).map((r) => <RecentRow key={r.run_id} run={r} waiting={waiting.has(r.run_id)} now={now} onOpen={() => onOpen(r.run_id)} />)
      )}
      <button type="button" className="side-run all" onClick={onAll} data-testid="recent-all">
        <span className="side-run-name">{t('nav.allRuns')}</span>
        {runs && !failed && <span className="side-run-age mono">{runs.length}</span>}
      </button>
    </div>
  )
}
