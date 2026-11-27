import { useEffect, useState, type ReactNode } from 'react'
import { decide } from './api'
import { approvalContext } from './approvalContext'
import { useConnectionReport } from './connection'
import { useRunStream } from './hooks'
import type { InboxItem, InboxKind } from './inbox'
import { acceptedNodes, activeNode, failureText, lastAttempt, nodeRows, stopNode } from './inboxRun'
import type { MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { formatDuration } from './insight'
import { nodeView, type RunView } from './reducer'
import { LoadingState } from './states'
import { ApprovalPill } from './status'
import type { GraphSummary } from './types'
import { Glyph, nodeGlyph, type GlyphKind } from './ui/glyphs'
import { Pill } from './ui/primitives'

// F2.5b: the selected Inbox item, as on the Tarmind Dashboard UX canvas. Everything
// below the headline is read from the run's own event stream (src/reducer.ts);
// the lists only say what to show.

type T = ReturnType<typeof useI18n>['t']

export const KIND_GLYPH: Record<InboxKind, GlyphKind> = { approval: 'approval', failed: 'bad', interrupted: 'paused', running: 'running' }

function Section({ title, children, testId }: { title: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <section className="inbox-sec" data-testid={testId}>
      <h3 className="inbox-label">{title}</h3>
      {children}
    </section>
  )
}

function Line({ glyph, name, muted = false, children }: { glyph?: ReactNode; name: ReactNode; muted?: boolean; children?: ReactNode }) {
  return (
    <li className="inbox-line">
      <span className="inbox-line-glyph">{glyph}</span>
      <span className={muted ? 'inbox-line-name mono mu' : 'inbox-line-name mono'}>{name}</span>
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

const verdictGlyph = (verdict: string): GlyphKind => (verdict === 'green' ? 'ok' : verdict === 'red' ? 'bad' : 'idle')

/** What the gate depends on (F2.10 approval context): the changed files and the check of each direct dependency's last attempt. */
function ApprovalBody({ item, run, graph }: { item: InboxItem; run: RunView; graph: GraphSummary | null }) {
  const { t } = useI18n()
  const node = item.approval?.node_id ?? ''
  const upstream = approvalContext(run, graph ? graph.edges.filter((e) => e.to === node).map((e) => e.from) : [])
  const observed = upstream.filter((u) => u.files && u.files.length > 0)
  const context = item.approval?.context ?? []
  return (
    <>
      {observed.length > 0 && (
        <Section title={t('inbox.sec.changed')} testId="inbox-changed">
          <ul className="inbox-lines">
            {observed.flatMap((u) => (u.files ?? []).map((f) => <Line key={`${u.nodeId}:${f}`} name={f}><span className="inbox-line-value mu">{u.nodeId}</span></Line>))}
            {observed.filter((u) => u.moreFiles > 0).map((u) => <Line key={`${u.nodeId}:more`} name={u.nodeId} muted><span className="inbox-line-value mu">{t('approval.context.more', { count: u.moreFiles })}</span></Line>)}
          </ul>
        </Section>
      )}
      {upstream.length > 0 && (
        <Section title={t('inbox.sec.evidence')} testId="inbox-evidence">
          <ul className="inbox-lines">
            {upstream.map((u) => (
              <Line key={u.nodeId} glyph={<Glyph kind={u.check ? verdictGlyph(u.check.verdict) : 'idle'} />} name={u.check ? `${u.nodeId} · ${u.check.checkId}` : u.nodeId}>
                <span className="inbox-line-value mu">{u.check ? u.check.reason : t('approval.context.noCheck')}</span>
              </Line>
            ))}
          </ul>
        </Section>
      )}
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
        <Section title={<>{t('inbox.sec.attemptsAt')} <span className="mono">{node.id}</span></>} testId="inbox-attempts">
          <ul className="inbox-lines">
            {node.attempts.map((a) => (
              <li key={a.id} className="inbox-line">
                <span className="inbox-attempt-no mono mu">{a.number}</span>
                <span className="inbox-line-value">{a.check?.reason ?? ''}</span>
                {a.outcome && <span className="mono mu">{a.outcome.replace('_', ' ')}</span>}
              </li>
            ))}
          </ul>
        </Section>
      )}
      {(failure.reason || failure.output) && (
        <Section title={t('inbox.sec.lastFailure')} testId="inbox-failure">
          <pre className="inbox-code">
            {failure.reason && <span className="r">{failure.reason}</span>}
            {failure.output}
          </pre>
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
              <span className="mu">{t('inbox.kept')}</span>
            </Line>
          ))}
          {stopped.map((n) => {
            const last = lastAttempt(n)
            return (
              <Line key={n.id} glyph={<Glyph kind="paused" />} name={n.id}>
                <span className="mu">{last ? t('inbox.stoppedAt', { number: last.number }) : t('inbox.stopped')}</span>
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
        {nodeRows(run, graph).map((r) => {
          const started = r.durationMs !== undefined
          const glyph = nodeGlyph(r.status)
          return (
            <Line key={r.id} glyph={glyph === 'idle' ? undefined : <Glyph kind={glyph} />} name={r.id} muted={!started}>
              <span className="sr-only">{t(`status.node.${r.status}` as MessageKey)}</span>
              {r.waitsFor.length > 0 && <span className="mu">{t('inbox.waitsFor', { nodes: r.waitsFor.join(', ') })}</span>}
              {r.status === 'retrying' && r.attempt && <Pill tone="w" small>{t('inbox.retryOf', r.attempt)}</Pill>}
              {started && r.status !== 'waiting_human' && <span className="mono mu">{formatDuration(r.durationMs!, locale)}</span>}
            </Line>
          )
        })}
      </ul>
    </Section>
  )
}

function DecisionBar({ item, decision, lost, onDecided, onNext }: { item: InboxItem; decision?: string; lost: boolean; onDecided: () => void; onNext: () => void }) {
  const { t } = useI18n()
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const a = item.approval
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
        <span className="mu inbox-small">{t('inbox.decided')}</span>
        <span className="inbox-bar-gap" />
        <button type="button" className="ib-btn pr" onClick={onNext} data-testid="inbox-next">{t('inbox.next')}</button>
      </div>
    )
  return (
    <div className="inbox-bar" data-testid="inbox-decide">
      <input type="text" aria-label={t('inbox.reason')} placeholder={t('inbox.reasonPlaceholder')} value={comment} maxLength={2048} onChange={(e) => setComment(e.target.value)} />
      <button type="button" className="ib-btn bad" disabled={busy || lost} onClick={() => void submit(false)} data-testid="inbox-reject">{t('approval.reject')}</button>
      <button type="button" className="ib-btn pr" disabled={busy || lost} onClick={() => void submit(true)} data-testid="inbox-approve">{t('approval.approve')}</button>
      {lost && <div className="mu inbox-bar-note">{t('approval.disabledLost')}</div>}
      {error && <div className="err inbox-bar-note" role="alert">{error}</div>}
    </div>
  )
}

function OpenBar({ summary, onOpen }: { summary: string; onOpen: () => void }) {
  const { t } = useI18n()
  const [copy, setCopy] = useState<'idle' | 'done' | 'failed'>('idle')
  async function copySummary() {
    try {
      await navigator.clipboard.writeText(summary)
      setCopy('done')
    } catch {
      setCopy('failed')
    }
  }
  return (
    <div className="inbox-bar">
      <button type="button" className="ib-btn ghost" onClick={() => void copySummary()} data-testid="inbox-copy">
        {copy === 'done' ? t('inbox.copied') : t('inbox.copySummary')}
      </button>
      {copy === 'failed' && <span className="err inbox-small" role="alert">{t('inbox.copyFailed')}</span>}
      <span className="inbox-bar-gap" />
      <button type="button" className="ib-btn pr" onClick={onOpen} data-testid="inbox-open-run">{t('approval.openRun')}</button>
    </div>
  )
}

export interface InboxDetailProps {
  item: InboxItem
  age: string
  onOpenRun: (id: string) => void
  onDecided: () => void
  /** The decision as the run's events recorded it (approval_decided), once it arrives. */
  onSettled: (decision: string) => void
  onNext: () => void
}

export function InboxDetail({ item, age, onOpenRun, onDecided, onSettled, onNext }: InboxDetailProps) {
  const { t } = useI18n()
  const { run, graph, lost, notFound } = useRunStream(item.runId)
  useConnectionReport('inbox-run', lost !== null)
  const { title, lede } = headline(item, run, t)
  const kind = t(`inbox.kind.${item.kind}` as MessageKey)
  const decision = item.approval ? nodeView(run, item.approval.node_id).approval?.decision : undefined
  useEffect(() => {
    if (decision) onSettled(decision)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [decision])
  return (
    <article className="inbox-item" data-testid="inbox-detail" data-kind={item.kind}>
      <div className="inbox-body">
        <header>
          <div className={`inbox-kind k-${item.kind}`}>
            <span>{kind}</span>
            {age && <span className="mu">· {age}</span>}
          </div>
          <h2>{title}</h2>
          <p className="inbox-lede">{lede}</p>
        </header>
        {notFound && <p className="mu">{t('run.notFound')}</p>}
        {item.kind === 'approval' && <ApprovalBody item={item} run={run} graph={graph} />}
        {item.kind === 'failed' && <FailedBody run={run} />}
        {item.kind === 'interrupted' && <InterruptedBody run={run} />}
        {item.kind === 'running' && <RunningBody run={run} graph={graph} />}
      </div>
      {item.kind === 'approval' ? (
        <DecisionBar item={item} decision={decision} lost={lost !== null} onDecided={onDecided} onNext={onNext} />
      ) : (
        <OpenBar summary={`${kind}: ${title}\n${lede}\n${item.graphId} · ${item.runId}`} onOpen={() => onOpenRun(item.runId)} />
      )}
    </article>
  )
}
