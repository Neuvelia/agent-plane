import { useState } from 'react'
import { decide } from './api'
import { approvalContext, type UpstreamContext } from './approvalContext'
import { AttemptCompare } from './AttemptCompare'
import { CheckResultView } from './check'
import { canCompare, defaultPair } from './compare'
import { useI18n } from './i18n/context'
import { displayStatus, nodeView, type RunView } from './reducer'
import { ApprovalPill, NodePill, OutcomePill, VerdictPill } from './status'
import { Button, Card, KeyValue, SegmentedControl, Tag } from './ui/primitives'

/** Checks and changed files of the nodes the gate depends on (F2.10), so the person decides with evidence at hand. */
export function ApprovalContext({ items, onOpenNode }: { items: UpstreamContext[]; onOpenNode?: (nodeId: string) => void }) {
  const { t } = useI18n()
  if (items.length === 0) return null
  return (
    <div className="approval-context stack" data-testid="approval-context">
      <div className="lb">{t('approval.context.title')}</div>
      {items.map((u) => (
        <div className="stack" key={u.nodeId} data-testid="approval-upstream">
          <div className="row between">
            <b className="mono">{u.nodeId}</b>
            {onOpenNode && (
              <Button size="sm" onClick={() => onOpenNode(u.nodeId)}>
                {t('approval.context.open', { node: u.nodeId })}
              </Button>
            )}
          </div>
          <div className="row" data-testid="approval-check">
            <span className="mu">{t('approval.context.check')}:</span>
            {u.check ? (
              <>
                <VerdictPill verdict={u.check.verdict} />
                <span className="mono">{u.check.checkId}</span>
                <span className="mu">{u.check.reason}</span>
              </>
            ) : (
              <span className="mu">{t('approval.context.noCheck')}</span>
            )}
          </div>
          <div data-testid="approval-files">
            <span className="mu">{t('approval.context.files')}: </span>
            {u.files ? (
              <>
                <span className="mu">{t('approval.context.filesCount', { count: u.files.length + u.moreFiles })}</span>
                <ul className="mono">
                  {u.files.map((f) => (
                    <li key={f}>{f}</li>
                  ))}
                </ul>
                {u.moreFiles > 0 && <div className="mu">{t('approval.context.more', { count: u.moreFiles })}</div>}
              </>
            ) : (
              <span className="mu">{t('approval.context.noFiles')}</span>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

export function ApprovalBox({
  runId,
  nodeRunId,
  prompt,
  context,
  disabled = false,
  upstream = [],
  onOpenNode,
}: {
  runId: string
  nodeRunId: string
  prompt: string
  context: { name: string; value: string }[]
  disabled?: boolean
  upstream?: UpstreamContext[]
  onOpenNode?: (nodeId: string) => void
}) {
  const { t } = useI18n()
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function submit(approve: boolean) {
    setBusy(true)
    setError('')
    const failure = await decide(runId, nodeRunId, approve, comment)
    // On success nothing is set here: the node leaves waiting_human when the
    // approval_decided event arrives on the stream.
    if (failure) setError(failure.message)
    setBusy(false)
  }
  return (
    <div className="approval" data-testid="approval-box">
      <div className="lb">{t('approval.needed')}</div>
      <div className="mu">{t('approval.reason')}</div>
      <p>{prompt || t('approval.defaultPrompt')}</p>
      <ApprovalContext items={upstream} onOpenNode={onOpenNode} />
      {context.length > 0 && <KeyValue rows={context.map((c) => ({ name: c.name, value: c.value }))} />}
      <textarea aria-label={t('approval.comment')} placeholder={t('approval.commentPlaceholder')} value={comment} maxLength={2048} onChange={(e) => setComment(e.target.value)} />
      <div className="row">
        <Button variant="danger" disabled={busy || disabled} onClick={() => submit(false)}>
          {t('approval.reject')}
        </Button>
        <Button variant="primary" disabled={busy || disabled} onClick={() => submit(true)}>
          {t('approval.approve')}
        </Button>
      </div>
      {disabled && <div className="mu">{t('approval.disabledLost')}</div>}
      {error && <div className="err">{error}</div>}
    </div>
  )
}

/** Detail of one node. Keyed by node id by the caller, so the chosen attempt and tab reset per node. */
export function NodePanel({
  run,
  nodeId,
  lost = false,
  kind,
  upstream,
  onOpenNode,
}: {
  run: RunView
  nodeId: string | null
  lost?: boolean
  kind?: string
  /** Direct dependencies of the node from the graph summary; the human gate context is built from them. */
  upstream?: readonly string[]
  onOpenNode?: (nodeId: string) => void
}) {
  const { t } = useI18n()
  const [tab, setTab] = useState<'attempts' | 'compare' | 'log'>('attempts')
  const [chosen, setChosen] = useState<string | null>(null)
  // The compared pair is view state only. Until the person picks, it follows the default (FR2).
  const [pair, setPair] = useState<{ from: string; to: string } | null>(null)
  if (!nodeId)
    return (
      <Card testId="node-panel-empty">
        <div className="mu empty-line">{t('node.select')}</div>
      </Card>
    )
  const n = nodeView(run, nodeId)
  const status = displayStatus(n)
  // Unless the person picked one, follow the latest attempt as new ones arrive.
  const shown = n.attempts.find((a) => a.id === chosen) ?? n.attempts[n.attempts.length - 1]
  const comparable = canCompare(n)
  const compared = pair ?? defaultPair(n)
  const view = tab === 'compare' && !comparable ? 'attempts' : tab
  return (
    <Card title={n.id} subtitle={kind ? <Tag>{kind}</Tag> : undefined} action={<NodePill status={status} />} testId="node-panel">
      {n.failureKind && (
        <div className="err">
          {n.failureKind}
          {n.diagnosticCodes.length > 0 && ` · ${n.diagnosticCodes.join(', ')}`}
        </div>
      )}
      {n.abandoned !== undefined && (
        <div className="err" data-testid="abandoned">
          {t('node.abandoned', { reason: n.abandoned })}
        </div>
      )}
      {status === 'waiting_human' && n.approval && run.runId && n.nodeRunId && (
        <ApprovalBox runId={run.runId} nodeRunId={n.nodeRunId} prompt={n.approval.prompt} context={n.approval.context} disabled={lost} upstream={approvalContext(run, upstream ?? [])} onOpenNode={onOpenNode} />
      )}
      {n.approval?.decision && (
        <div className="decision row" data-testid="decision">
          <span>{t('node.decision')}:</span>
          <ApprovalPill decision={n.approval.decision} />
          {n.approval.comment && <span className="mu">{n.approval.comment}</span>}
        </div>
      )}
      <SegmentedControl
        label={t('node.view')}
        value={view}
        onChange={setTab}
        segments={[
          { value: 'attempts', label: n.attempts.length ? t('node.attemptsCount', { count: n.attempts.length }) : t('node.attempts'), testId: 'tab-attempts' },
          ...(comparable ? [{ value: 'compare' as const, label: t('compare.tab'), testId: 'tab-compare' }] : []),
          { value: 'log', label: t('node.log'), testId: 'tab-log' },
        ]}
      />
      {view === 'compare' && compared && <AttemptCompare run={run} node={n} kind={kind} from={compared.from} to={compared.to} onChange={setPair} />}
      {view === 'attempts' &&
        (n.attempts.length === 0 || !shown ? (
          n.check ? <CheckResultView check={n.check} /> : <div className="mu">{t('node.noLoop')}</div>
        ) : (
          <>
            {n.attempts.length > 1 && (
              <SegmentedControl
                label={t('node.attemptsOf')}
                value={shown.id}
                onChange={setChosen}
                segments={n.attempts.map((a) => ({ value: a.id, label: t('node.attemptTab', { n: a.number }), testId: `attempt-tab-${a.number}` }))}
              />
            )}
            <div className="attempt" key={shown.id} data-testid="attempt">
              <div className="row between">
                <b>{t('node.attempt', { n: shown.number, max: shown.max })}</b>
                <OutcomePill outcome={shown.outcome} />
              </div>
              {shown.check && <CheckResultView check={shown.check} />}
              {shown.log && <pre className="log small">{shown.log}</pre>}
            </div>
          </>
        ))}
      {view === 'log' && <pre className="log" data-testid="node-log" tabIndex={0}>{n.log || t('node.noLog')}</pre>}
    </Card>
  )
}
