import { scopeOf, type NotObserved } from './compare'
import { formatDateTime, type MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { formatDuration } from './insight'
import type { Activity, ActivityItem, AttemptInput, CheckScopeView, CheckView, NodeView, RunView } from './reducer'
import { KeyValue, Tag } from './ui/primitives'

// Node detail building blocks of F2.7. Everything shown is untrusted run data
// (paths, tool and command names, assistant text, reasons) and is rendered as
// text: no HTML, no links built from values, nothing that opens a path.

/** Node-run ID, start and end, duration and adapter (FR1). A running node shows no duration. */
export function FactsHeader({ n }: { n: NodeView }) {
  const { t, locale } = useI18n()
  const unknown = t('kpi.unknown')
  const running = n.finishedAt === undefined && n.startedAt !== undefined && (n.status === 'running' || n.status === 'waiting_human')
  const at = (iso: string | undefined) => (iso ? formatDateTime(locale, iso) : unknown)
  return (
    <div data-testid="node-facts">
      <KeyValue
        rows={[
          { name: t('facts.nodeRun'), value: <span className="mono" data-testid="fact-node-run">{n.nodeRunId ?? unknown}</span> },
          { name: t('facts.started'), value: <span data-testid="fact-started">{at(n.startedAt)}</span> },
          { name: t('facts.finished'), value: <span data-testid="fact-finished">{running ? t('facts.running') : at(n.finishedAt)}</span> },
          { name: t('facts.duration'), value: <span data-testid="fact-duration">{running ? t('facts.running') : n.durationMs === undefined ? unknown : formatDuration(n.durationMs, locale)}</span> },
          { name: t('facts.adapter'), value: <span className="mono" data-testid="fact-adapter">{n.adapter ? `${n.adapter.id}${n.adapter.version ? ` ${n.adapter.version}` : ''}` : unknown}</span> },
        ]}
      />
    </div>
  )
}

function ScopeDetail({ scope }: { scope: CheckScopeView }) {
  const { t } = useI18n()
  return (
    <div className="mu" data-testid="check-scope-detail">
      <span>{t('check.scopeMode', { mode: scope.mode })}</span>
      {scope.observedCount !== undefined && <span> · {t('check.scopeObserved', { count: scope.observedCount })}</span>}
      <div data-testid="check-allowed-paths">
        {scope.redacted ? t('check.allowedRedacted') : (scope.allowedPaths ?? []).length === 0 ? t('check.allowedNone') : `${t('check.allowed')}: ${(scope.allowedPaths ?? []).join(', ')}`}
      </div>
    </div>
  )
}

/** Checked inputs and, for changed_files_scope, the mode, observed count and allowed paths (FR3). */
export function CheckScopeInfo({ check }: { check: CheckView }) {
  const { t } = useI18n()
  return (
    <>
      {check.checkedInputs.length > 0 && (
        <div className="mu" data-testid="check-inputs">
          {t('check.inputs')}: <span className="mono">{check.checkedInputs.join(', ')}</span>
        </div>
      )}
      {check.scope && <ScopeDetail scope={check.scope} />}
    </>
  )
}

const REASON_KEY: Record<NotObserved, MessageKey> = {
  shell: 'compare.notObserved.shell',
  failed: 'compare.notObserved.failed',
  running: 'compare.notObserved.running',
  not_recorded: 'compare.notObserved.not_recorded',
}

/** Why a node with no loop has no changed-file list. Never "0 files". */
export function nodeNotObserved(n: NodeView, kind: string | undefined, runActive: boolean): NotObserved {
  if (kind === 'shell') return 'shell'
  if (n.status === 'failed' || n.status === 'cancelled') return 'failed'
  if (runActive && n.status !== 'succeeded' && n.status !== 'skipped') return 'running'
  return 'not_recorded'
}

/**
 * Observed paths, the diff summary (counts only: contents are not recorded until F2.8b) and the
 * provider's own claims under a separate, unverified label (FR5-7). The claims never feed the counts.
 */
export function ChangedFilesBlock({ files, reason, check, activity }: { files: string[] | undefined; reason: NotObserved; check: CheckView | undefined; activity: Activity }) {
  const { t } = useI18n()
  const scope = scopeOf(check)
  const outside = scope.kind === 'checked' ? new Set(scope.paths) : undefined
  const reported = activity.items.filter((i): i is Extract<ActivityItem, { kind: 'file' }> => i.kind === 'file')
  return (
    <section className="changed" data-testid="changed-files" aria-label={t('detail.changedFiles')}>
      <div className="lb">{t('detail.changedFiles')}</div>
      {files ? (
        <>
          <div className="row">
            <span data-testid="changed-count">{t('compare.filesCount', { count: files.length })}</span>
            <Tag>{t('detail.observedByGit')}</Tag>
          </div>
          <ul className="paths" data-testid="changed-list">
            {files.map((p) => (
              <li key={p} className="mono">
                {p}
                {outside?.has(p) ? ` (${t('detail.outOfScope')})` : ''}
              </li>
            ))}
          </ul>
        </>
      ) : (
        <div className="mu" data-testid="changed-not-observed">
          {t(REASON_KEY[reason])}
        </div>
      )}
      <div className="mu" data-testid="diff-summary">
        {files ? t('detail.diffObserved', { count: files.length }) : t('detail.diffUnknown')}
        {outside ? ` · ${t('detail.diffOutside', { count: outside.size })}` : ''}
        {' · '}
        {t('detail.contentsNotRecorded')}
      </div>
      {reported.length > 0 && (
        <div data-testid="reported-files">
          <div className="lb">{t('detail.reportedFiles')}</div>
          <ul className="paths">
            {reported.map((r, i) => (
              <li key={i} className="mono">
                {r.path} <span className="mu">({r.action})</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}

/** Short fingerprint, intended scope and reference flags of an attempt's input (FR15). */
export function AttemptInputView({ input }: { input: AttemptInput | undefined }) {
  const { t } = useI18n()
  if (!input) return null
  return (
    <div className="mu" data-testid="attempt-input">
      <div>
        {t('detail.taskFingerprint')}: <span className="mono" data-testid="attempt-fingerprint">{input.fingerprint.slice(0, 12)}</span>
      </div>
      <div data-testid="attempt-scope">{input.scopeOmitted || !input.scope ? t('detail.scopeOmitted') : input.scope.length === 0 ? t('detail.scopeNone') : `${t('detail.intendedScope')}: ${input.scope.join(', ')}`}</div>
      <div>
        {t('detail.contextRefs', { present: input.contextRefsPresent ? t('detail.yes') : t('detail.no') })} · {t('detail.environmentRefs', { present: input.environmentRefsPresent ? t('detail.yes') : t('detail.no') })}
      </div>
    </div>
  )
}

function ActivityLine({ item, interrupted }: { item: ActivityItem; interrupted: boolean }) {
  const { t } = useI18n()
  switch (item.kind) {
    case 'text':
      return <li data-testid="activity-text"><span className="lb">{t('activity.text')}</span> <span className="activity-text">{item.text}</span></li>
    case 'tool':
      return <li data-testid="activity-tool"><span className="lb">{t('activity.tool')}</span> <span className="mono">{item.name}</span> <span className="mu">{unfinished(item.status, interrupted) ? t('activity.unfinished') : item.status}</span></li>
    case 'command':
      return (
        <li data-testid="activity-command">
          <span className="lb">{t('activity.command')}</span> <span className="mono">{item.commandId}</span> <span className="mu">{unfinished(item.status, interrupted) ? t('activity.unfinished') : item.status}</span>
          {item.exitCode !== undefined && <span className="mono"> {t('activity.exit', { code: item.exitCode })}</span>}
        </li>
      )
    case 'file':
      return <li data-testid="activity-file"><span className="lb">{t('activity.file')}</span> <span className="mono">{item.path}</span> <span className="mu">({item.action}; {t('detail.reportedUnverified')})</span></li>
    case 'status':
      return <li data-testid="activity-status"><span className="lb">{t('activity.status')}</span> <span className="mono">{item.code}</span></li>
    default:
      return <li data-testid="activity-omitted">{t('activity.omitted', { retained: item.retainedEvents, live: item.liveEvents })}</li>
  }
}

const unfinished = (status: string, interrupted: boolean): boolean => interrupted && status === 'in_progress'

/** The Activity tab: the node's progress in order, with the attempt each item belongs to (FR9). */
export function ActivityView({ run, n }: { run: RunView; n: NodeView }) {
  const { t } = useI18n()
  const interrupted = run.status === 'interrupted'
  const groups = [...n.attempts.map((a) => ({ label: t('node.attemptTab', { n: a.number }), activity: a.activity })), { label: n.attempts.length > 0 ? t('activity.nodeLevel') : '', activity: n.activity }].filter((g) => g.activity.items.length > 0 || g.activity.dropped > 0)
  if (groups.length === 0)
    return (
      <div className="mu" data-testid="activity-empty">
        {run.schemaVersion !== undefined && run.schemaVersion < 3 ? t('activity.oldSchema') : t('activity.none')}
      </div>
    )
  return (
    <div data-testid="activity">
      {groups.map((g, gi) => (
        <section key={gi} aria-label={g.label || t('node.activity')}>
          {g.label && <div className="lb">{g.label}</div>}
          {g.activity.dropped > 0 && <div className="mu" data-testid="activity-dropped">{t('activity.dropped', { count: g.activity.dropped })}</div>}
          <ol className="activity-list">
            {g.activity.items.map((item, i) => (
              <ActivityLine key={i} item={item} interrupted={interrupted} />
            ))}
          </ol>
        </section>
      ))}
    </div>
  )
}
