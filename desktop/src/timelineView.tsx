import { useI18n } from './i18n/context'
import { formatDuration, MAX_TIMELINE_COLUMNS, type Timeline, type TimelineRow } from './insight'
import type { MessageKey } from './i18n'
import type { TimelineSegment } from './timelineSegments'

// The overview timeline (F2.6 requirement 6): one native button per node, dots
// toned by what the node was doing, and a visually hidden table with the same
// segments as the text alternative. Everything is text or a data attribute; node
// IDs and outcomes are untrusted and never become markup.

function rowName(row: TimelineRow, shown: string, t: ReturnType<typeof useI18n>['t'], locale: ReturnType<typeof useI18n>['locale']): string {
  return t('timeline.row', {
    node: shown,
    attempts: row.attempts,
    start: formatDuration(row.startSeconds * 1000, locale),
    duration: row.durationMs === undefined ? t('kpi.unknown') : formatDuration(row.durationMs, locale),
    outcome: t(`status.node.${row.status}` as MessageKey),
  })
}

function SegmentTable({ rows }: { rows: TimelineRow[] }) {
  const { t, locale } = useI18n()
  const at = (seconds: number) => formatDuration(seconds * 1000, locale)
  const describe = (s: TimelineSegment) => `${t(`timeline.segment.${s.kind}` as MessageKey)}${s.provisional ? ` (${t('timeline.provisional')})` : ''}`
  return (
    <table className="sr-only" aria-label={t('timeline.table')} data-testid="timeline-table">
      <thead>
        <tr>
          {(['node', 'segment', 'attempt', 'outcome', 'from', 'to'] as const).map((c) => (
            <th key={c}>{t(`timeline.col.${c}` as MessageKey)}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.flatMap((row) =>
          row.segments.map((s, i) => (
            <tr key={`${row.name}:${i}`}>
              <td>{row.name}</td>
              <td>{describe(s)}</td>
              <td>{s.attempt ?? ''}</td>
              <td>{s.outcome ?? ''}</td>
              <td>{at(s.startSeconds)}</td>
              <td>{at(s.endSeconds)}</td>
            </tr>
          )),
        )}
      </tbody>
    </table>
  )
}

export function NodeTimeline({ timeline, selected, critical, hidden, onSelect, caption }: { timeline: Timeline; selected: string | null; critical: ReadonlySet<string>; hidden: ReadonlySet<string>; onSelect: (id: string) => void; caption?: string }) {
  const { t, locale } = useI18n()
  // The selected node is never hidden, so a filter cannot make the open node disappear.
  const rows = timeline.rows.filter((r) => !hidden.has(r.name) || r.name === selected)
  return (
    <div className="timeline" data-testid="timeline">
      {caption && <span className="mu small-print">{caption}</span>}
      {rows.map((r) => (
        <button
          key={r.name}
          type="button"
          className={`tl-row tl-btn${r.name === selected ? ' sel' : ''}`}
          aria-label={rowName(r, critical.has(r.name) ? `${r.name} ${t('overview.criticalBadge')}` : r.name, t, locale)}
          aria-pressed={r.name === selected}
          onClick={() => onSelect(r.name)}
          data-testid={`timeline-row-${r.name}`}
        >
          <span className="mono mu tl-name">
            {r.name}{' '}
            {critical.has(r.name) && <span className="tag" data-testid={`timeline-critical-${r.name}`}>{t('overview.criticalBadge')}</span>}
          </span>
          <span className="tl-dots" aria-hidden="true">
            {r.cells.slice(0, MAX_TIMELINE_COLUMNS).map((tone, i) => (
              <span key={i} className={`dot sm ${tone ? `d-t-${tone}` : 'd-empty'}`} />
            ))}
          </span>
        </button>
      ))}
      <span className="mu small-print" data-testid="timeline-legend">
        {t('timeline.legend')}
      </span>
      <SegmentTable rows={rows} />
    </div>
  )
}
