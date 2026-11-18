import { useId, type ReactNode } from 'react'
import { EvidenceList } from './check'
import { compareModel, type CompareSide, type FileRow } from './compare'
import { useI18n } from './i18n/context'
import type { MessageKey } from './i18n'
import type { NodeView, RunView } from './reducer'
import { OutcomePill, VerdictPill } from './status'
import { Button, Field, Pill } from './ui/primitives'

// F2.8a: two attempts of one node side by side, from recorded events only.
// Every value shown is untrusted run data and is rendered as text.

const NOT_OBSERVED: Record<NonNullable<CompareSide['notObserved']>, MessageKey> = {
  shell: 'compare.notObserved.shell',
  failed: 'compare.notObserved.failed',
  running: 'compare.notObserved.running',
  not_recorded: 'compare.notObserved.not_recorded',
}

function Files({ side, other }: { side: CompareSide; other: CompareSide }) {
  const { t } = useI18n()
  if (!side.files) return <span className="mu" data-testid="files-not-observed">{t(NOT_OBSERVED[side.notObserved ?? 'not_recorded'])}</span>
  if (side.files.length === 0) return <span>{t('compare.filesCount', { count: 0 })}</span>
  return (
    <div className="cmp-files">
      <span>{t('compare.filesCount', { count: side.files.length })}</span>
      <ul>
        {side.files.map((f: FileRow) => (
          <li key={f.path} data-testid="changed-file">
            <span className="mono">{f.path}</span>
            {f.isNew && (
              <Pill tone="w" small testId="badge-new">
                {t('compare.newFile', { n: other.attempt.number })}
              </Pill>
            )}
            {f.outsideScope && (
              <Pill tone="x" small testId="badge-outside">
                {t('compare.outsideScope')}
              </Pill>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

function Scope({ side }: { side: CompareSide }) {
  const { t } = useI18n()
  const s = side.scope
  if (s.kind === 'not_checked') return <span className="mu" data-testid="scope-not-checked">{t('compare.scopeNotChecked')}</span>
  if (s.kind === 'indeterminate') return <span className="err" data-testid="scope-indeterminate">{t('compare.scopeIndeterminate')}</span>
  if (s.paths.length === 0 && s.omitted === 0) return <span>{t('compare.scopeClean')}</span>
  return (
    <div className="cmp-files" data-testid="scope-outside">
      <span>{t('compare.scopeOutside', { count: s.paths.length + s.omitted })}</span>
      <ul>
        {s.paths.map((p) => (
          <li key={p} className="mono">
            {p}
          </li>
        ))}
      </ul>
      {s.omitted > 0 && <span className="mu">{t('compare.scopeOmitted', { count: s.omitted })}</span>}
    </div>
  )
}

function Row({ label, from, to, testId }: { label: string; from: ReactNode; to: ReactNode; testId: string }) {
  return (
    <tr data-testid={testId}>
      <th scope="row" className="lb">
        {label}
      </th>
      <td>{from}</td>
      <td>{to}</td>
    </tr>
  )
}

export function AttemptCompare({ run, node, kind, from, to, onChange }: { run: RunView; node: NodeView; kind?: string; from: string; to: string; onChange: (pair: { from: string; to: string }) => void }) {
  const { t } = useI18n()
  const fromId = useId()
  const toId = useId()
  const model = compareModel(run, node, from, to, kind)
  if (!model) return null
  const { from: a, to: b, feedback } = model
  // Outcomes are machine values from the engine and are shown literally, as OutcomePill does.
  const option = (id: string) => {
    const x = node.attempts.find((y) => y.id === id)!
    return t('compare.option', { n: x.number, outcome: x.outcome ?? t(run.status === 'running' ? 'compare.running' : 'compare.interrupted') })
  }
  const unknown = t('compare.unknown')
  const notChecked = <span className="mu">{t('compare.notChecked')}</span>
  const verdict = (s: CompareSide) => (s.check ? <VerdictPill verdict={s.check.verdict} /> : notChecked)
  const reason = (s: CompareSide) => (s.check ? <span data-testid="cmp-reason">{s.check.reason}</span> : notChecked)
  const evidence = (s: CompareSide) => (s.check ? s.check.evidence.length ? <EvidenceList check={s.check} /> : <span className="mu">{t('compare.noEvidence')}</span> : notChecked)
  const feedbackCell = (s: CompareSide) => (s.feedbackFrom === undefined ? t('compare.feedbackNone') : s.feedbackFrom === 0 ? t('compare.feedbackMissingShort') : t('compare.feedbackFrom', { n: s.feedbackFrom }))
  const duration = (v?: number) => (v === undefined ? <span className="mu">{unknown}</span> : <span className="mono">{t('compare.ms', { v })}</span>)
  return (
    <div className="cmp" data-testid="attempt-compare">
      <div className="cmp-controls">
        <Field label={t('compare.from')} htmlFor={fromId}>
          <select id={fromId} value={a.attempt.id} onChange={(e) => onChange({ from: e.target.value, to: b.attempt.id })} data-testid="compare-from">
            {node.attempts.map((x) => (
              <option key={x.id} value={x.id}>
                {option(x.id)}
              </option>
            ))}
          </select>
        </Field>
        <Button size="sm" onClick={() => onChange({ from: b.attempt.id, to: a.attempt.id })} data-testid="compare-swap">
          {t('compare.swap')}
        </Button>
        <Field label={t('compare.to')} htmlFor={toId}>
          <select id={toId} value={b.attempt.id} onChange={(e) => onChange({ from: a.attempt.id, to: e.target.value })} data-testid="compare-to">
            {node.attempts.map((x) => (
              <option key={x.id} value={x.id}>
                {option(x.id)}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <div className="cmp-scroll">
        <table className="cmp-table">
          <caption className="sr-only">{t('compare.title')}</caption>
          <thead>
            <tr>
              <th scope="col">
                <span className="sr-only">{t('compare.fact')}</span>
              </th>
              <th scope="col" data-testid="cmp-head-from">
                {t('compare.attemptHead', { n: a.attempt.number })}
              </th>
              <th scope="col" data-testid="cmp-head-to">
                {t('compare.attemptHead', { n: b.attempt.number })}
              </th>
            </tr>
          </thead>
          <tbody>
            <Row label={t('compare.outcome')} testId="cmp-outcome" from={<OutcomePill outcome={a.outcome} />} to={<OutcomePill outcome={b.outcome} />} />
            <Row label={t('compare.verdict')} testId="cmp-verdict" from={verdict(a)} to={verdict(b)} />
            <Row label={t('compare.reason')} testId="cmp-reason-row" from={reason(a)} to={reason(b)} />
            <Row label={t('compare.evidence')} testId="cmp-evidence" from={evidence(a)} to={evidence(b)} />
            <Row label={t('compare.attemptDuration')} testId="cmp-attempt-duration" from={duration(a.attemptMs)} to={duration(b.attemptMs)} />
            <Row label={t('compare.checkDuration')} testId="cmp-check-duration" from={a.check ? duration(a.check.durationMs) : notChecked} to={b.check ? duration(b.check.durationMs) : notChecked} />
            <Row label={t('compare.files')} testId="cmp-files" from={<Files side={a} other={b} />} to={<Files side={b} other={a} />} />
            <Row label={t('compare.scope')} testId="cmp-scope" from={<Scope side={a} />} to={<Scope side={b} />} />
            <Row label={t('compare.feedback')} testId="cmp-feedback" from={feedbackCell(a)} to={feedbackCell(b)} />
          </tbody>
        </table>
      </div>
      {(a.files || b.files) && <p className="mu fine">{t('compare.persistNote')}</p>}
      {(a.accepted || b.accepted) && (
        <p className="cmp-accepted" data-testid="cmp-accepted">
          <Pill tone="p" small>
            {t('compare.attemptHead', { n: (a.accepted ? a : b).attempt.number })}
          </Pill>{' '}
          {t('compare.accepted')}
        </p>
      )}
      {feedback && (
        <section className="cmp-feedback" data-testid="cmp-feedback-section" aria-label={t('compare.feedbackTitle', { n: b.attempt.number })}>
          <div className="lb">{t('compare.feedbackTitle', { n: b.attempt.number })}</div>
          <div className="mono">
            {t('compare.previousAttempt')}: {feedback.fromNumber ?? unknown}
          </div>
          <div className="mono" data-testid="cmp-previous-check">
            {t('compare.previousCheck')}: {feedback.previousCheckId}
          </div>
          {feedback.check ? (
            <>
              <div data-testid="cmp-feedback-reason">{feedback.check.reason}</div>
              <EvidenceList check={feedback.check} />
            </>
          ) : (
            <div className="err">{t('compare.feedbackMissing')}</div>
          )}
          <p className="mu fine">{t('compare.feedbackHelp')}</p>
        </section>
      )}
      <p className="cmp-contents mu" data-testid="cmp-no-contents">
        {t('compare.noContents')}
      </p>
    </div>
  )
}
