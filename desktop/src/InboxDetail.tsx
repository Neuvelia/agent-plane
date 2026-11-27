import { useState, type ReactNode } from 'react'
import { decide } from './api'
import { useConnectionReport } from './connection'
import { useRunStream } from './hooks'
import type { InboxItem, InboxKind } from './inbox'
import { acceptedNodes, activeNode, changedFiles, failureText, lastAttempt, latestChecks, nodeRows, stopNode } from './inboxRun'
import type { MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { formatDuration } from './insight'
import { nodeView, type RunView } from './reducer'
import { LoadingState } from './states'
import { ApprovalPill, OutcomePill } from './status'
import type { GraphSummary } from './types'
import { Glyph, nodeGlyph, type GlyphKind } from './ui/glyphs'
import { Button, Pill } from './ui/primitives'

// F2.5b: the selected Inbox item. Everything below the headline is read from
// the run's own event stream (src/reducer.ts); the lists only say what to show.

type T = ReturnType<typeof useI18n>['t']

export const KIND_GLYPH: Record<InboxKind, GlyphKind> = { approval: 'approval', failed: 'bad', interrupted: 'paused', running: 'running' }

function Section({ title, children, testId }: { title: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <section className="inbox-sec" data-testid={testId}>
      <h3 className="lb">{title}</h3>
      {children}
    </section>
  )
}

function Line({ glyph, name, children }: { glyph?: ReactNode; name: ReactNode; children?: ReactNode }) {
  return (
    <li className="inbox-line">
      <span className="inbox-line-glyph">{glyph}</span>
      <span className="inbox-line-name mono">{name}</span>
      {children}
    </li>
  )
}

/** The headline and the sentence under it, from the item and what the events say so far. */
function headline(item: InboxItem, run: RunView, t: T): { title: string; lede: string } {
  const graph = item.graphId
  if (item.kind === 'approval') {
    const a = item.approval
    return { title: a?.prompt || t('approval.defaultPrompt'), lede: t('inbox.lede.approval', { node: a?.node_id ?? '' }) }
  }
  if (item.kind === 'interrupted') return { title: t('inbox.head.interrupted', { graph }), lede: t('inbox.lede.interrupted', { sequence: run.lastSequence }) }
  if (item.kind === 'running') {
    const active = activeNode(run)
    const last = active && lastAttempt(active.node)
    const title = active ? t('inbox.head.runningAt', { graph, node: active.node.id }) : t('inbox.head.running', { graph })
    const lede =
      active?.status === 'retrying' && last ? t('inbox.lede.retrying', { number: last.number, max: last.max, node: active.node.id })
      : active?.status === 'waiting_human' ? t('inbox.lede.waitingHuman', { node: active.node.id })
      : t('inbox.lede.running')
    return { title, lede }
  }
  const stopped = stopNode(run)
  if (!stopped) return { title: t('inbox.head.failed', { graph }), lede: t('inbox.lede.runFailed') }
  const last = lastAttempt(stopped)
  const lede = last && last.max > 1 && stopped.attempts.length >= last.max ? t('inbox.lede.attemptsUsed', { used: stopped.attempts.length, max: last.max }) : t('inbox.lede.nodeFailed', { node: stopped.id })
  return { title: t('inbox.head.failedAt', { graph, node: stopped.id }), lede }
}

function ApprovalBody({ item, run }: { item: InboxItem; run: RunView }) {
  const { t } = useI18n()
  const context = item.approval?.context ?? []
  const checks = latestChecks(run)
  const files = changedFiles(run)
  return (
    <>
      {context.length > 0 && (
        <Section title={t('approvals.context')} testId="inbox-context">
          <ul className="inbox-lines">
            {context.map((c) => (
              <Line key={c.name} name={c.name}>
                <span className="inbox-line-value">{c.value}{c.truncated ? '…' : ''}</span>
              </Line>
            ))}
          </ul>
        </Section>
      )}
      {files.length > 0 && (
        <Section title={t('inbox.sec.changed')} testId="inbox-changed">
          <ul className="inbox-lines">{files.map((f) => <Line key={f} name={f} />)}</ul>
        </Section>
      )}
      {checks.length > 0 && (
        <Section title={t('inbox.sec.evidence')} testId="inbox-evidence">
          <ul className="inbox-lines">
            {checks.map(({ nodeId, check }) => (
              <Line key={nodeId} glyph={<Glyph kind={check.verdict === 'green' ? 'ok' : check.verdict === 'red' ? 'bad' : 'idle'} />} name={`${nodeId} · ${check.checkId}`}>
                <span className="inbox-line-value mu">{check.reason}</span>
              </Line>
            ))}
          </ul>
        </Section>
      )}
    </>
  )
}

function FailedBody({ run }: { run: RunView }) {
  const { t } = useI18n()
  const node = stopNode(run)
  if (!node) return null
  const failure = failureText(node)
  return (
    <>
      {node.attempts.length > 0 && (
        <Section title={t('inbox.sec.attempts', { node: node.id })} testId="inbox-attempts">
          <ul className="inbox-lines">
            {node.attempts.map((a) => (
              <Line key={a.id} name={a.number}>
                <span className="inbox-line-value">{a.check?.reason ?? ''}</span>
                <OutcomePill outcome={a.outcome} />
              </Line>
            ))}
          </ul>
        </Section>
      )}
      {(failure.reason || failure.output) && (
        <Section title={t('inbox.sec.lastFailure')} testId="inbox-failure">
          {failure.reason && <p className="inbox-reason">{failure.reason}</p>}
          {failure.output && <pre className="inbox-code">{failure.output}</pre>}
        </Section>
      )}
    </>
  )
}

function InterruptedBody({ run }: { run: RunView }) {
  const { t } = useI18n()
  const stopped = run.order.map((id) => nodeView(run, id)).filter((n) => n.status === 'interrupted')
  return (
    <>
      <Section title={t('inbox.sec.survived')} testId="inbox-survived">
        <ul className="inbox-lines">
          {acceptedNodes(run).map((id) => (
            <Line key={id} glyph={<Glyph kind="ok" />} name={id}>
              <span className="inbox-line-value mu">{t('inbox.kept')}</span>
            </Line>
          ))}
          {stopped.map((n) => {
            const last = lastAttempt(n)
            return (
              <Line key={n.id} glyph={<Glyph kind="paused" />} name={n.id}>
                <span className="inbox-line-value mu">{last ? t('inbox.stoppedAt', { number: last.number }) : t('inbox.stopped')}</span>
              </Line>
            )
          })}
        </ul>
      </Section>
      <p className="mu inbox-note">{t('inbox.noResume')}</p>
    </>
  )
}

function RunningBody({ run, graph }: { run: RunView; graph: GraphSummary | null }) {
  const { t, locale } = useI18n()
  if (!graph) return <LoadingState />
  return (
    <Section title={t('inbox.sec.nodes')} testId="inbox-nodes">
      <ul className="inbox-lines">
        {nodeRows(run, graph).map((r) => (
          <Line key={r.id} glyph={<Glyph kind={nodeGlyph(r.status)} />} name={r.id}>
            <span className="inbox-line-value mu">{r.waitsFor.length > 0 ? t('inbox.waitsFor', { nodes: r.waitsFor.join(', ') }) : t(`status.node.${r.status}` as MessageKey)}</span>
            {r.status === 'retrying' && r.attempt && <Pill tone="w" small>{t('inbox.attemptOf', r.attempt)}</Pill>}
            {r.durationMs !== undefined && r.status !== 'waiting_human' && <span className="mono mu">{formatDuration(r.durationMs, locale)}</span>}
          </Line>
        ))}
      </ul>
    </Section>
  )
}

function DecisionBar({ item, run, lost, onDecided, onNext, onOpenRun }: { item: InboxItem; run: RunView; lost: boolean; onDecided: () => void; onNext: () => void; onOpenRun: () => void }) {
  const { t } = useI18n()
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const a = item.approval
  const decision = a ? nodeView(run, a.node_id).approval?.decision : undefined
  async function submit(approve: boolean) {
    if (!a) return
    setBusy(true)
    setError('')
    const failure = await decide(a.run_id, a.node_run_id, approve, comment)
    setBusy(false)
    // The decision is shown only when approval_decided arrives on the stream.
    if (failure) setError(failure.message)
    else onDecided()
  }
  if (decision)
    return (
      <div className="inbox-bar" data-testid="inbox-decided">
        <ApprovalPill decision={decision} />
        <span className="mu">{t('inbox.decided')}</span>
        <span className="inbox-bar-gap" />
        <Button onClick={onOpenRun} data-testid="inbox-open-run">{t('approval.openRun')}</Button>
        <Button variant="primary" onClick={onNext} data-testid="inbox-next">{t('inbox.next')}</Button>
      </div>
    )
  return (
    <div className="inbox-bar" data-testid="inbox-decide">
      <input type="text" aria-label={t('inbox.reason')} placeholder={t('inbox.reasonPlaceholder')} value={comment} maxLength={2048} onChange={(e) => setComment(e.target.value)} />
      <Button variant="danger" disabled={busy || lost} onClick={() => void submit(false)} data-testid="inbox-reject">{t('approval.reject')}</Button>
      <Button variant="primary" disabled={busy || lost} onClick={() => void submit(true)} data-testid="inbox-approve">{t('approval.approve')}</Button>
      {lost && <div className="mu inbox-bar-note">{t('approval.disabledLost')}</div>}
      {error && <div className="err inbox-bar-note" role="alert">{error}</div>}
    </div>
  )
}

export function InboxDetail({ item, age, onOpenRun, onDecided, onNext }: { item: InboxItem; age: string; onOpenRun: (id: string) => void; onDecided: () => void; onNext: () => void }) {
  const { t } = useI18n()
  const { run, graph, lost, notFound } = useRunStream(item.runId)
  useConnectionReport('inbox-run', lost !== null)
  const { title, lede } = headline(item, run, t)
  const open = () => onOpenRun(item.runId)
  return (
    <article className="inbox-item" data-testid="inbox-detail" data-kind={item.kind}>
      <div className="inbox-body">
        <header>
          <div className={`inbox-kind k-${item.kind}`}>
            <Glyph kind={KIND_GLYPH[item.kind]} />
            <span>{t(`inbox.kind.${item.kind}` as MessageKey)}</span>
            <span className="mu">· {item.graphId}{age ? ` · ${age}` : ''}</span>
          </div>
          <h2>{title}</h2>
          <p className="inbox-lede">{lede}</p>
        </header>
        {notFound && <p className="mu">{t('run.notFound')}</p>}
        {item.kind === 'approval' && <ApprovalBody item={item} run={run} />}
        {item.kind === 'failed' && <FailedBody run={run} />}
        {item.kind === 'interrupted' && <InterruptedBody run={run} />}
        {item.kind === 'running' && <RunningBody run={run} graph={graph} />}
      </div>
      {item.kind === 'approval' ? (
        <DecisionBar item={item} run={run} lost={lost !== null} onDecided={onDecided} onNext={onNext} onOpenRun={open} />
      ) : (
        <div className="inbox-bar">
          <span className="inbox-bar-gap" />
          <Button variant="primary" onClick={open} data-testid="inbox-open-run">{t('approval.openRun')}</Button>
        </div>
      )}
    </article>
  )
}
