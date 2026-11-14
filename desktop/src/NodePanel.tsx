import { useState } from 'react'
import { decide } from './api'
import { useI18n } from './i18n/context'
import { displayStatus, nodeView, type Attempt, type RunView } from './reducer'
import { ApprovalPill, NodePill, OutcomePill, VerdictPill } from './status'
import { Button, Card, KeyValue, SegmentedControl, Tag } from './ui/primitives'

function Evidence({ attempt }: { attempt: Attempt }) {
  const check = attempt.check
  if (!check) return null
  return (
    <div className="check" data-testid="check-result">
      <div className="row">
        <VerdictPill verdict={check.verdict} />
        <span className="mono">{check.checkId}</span>
        <Tag>{check.kind}</Tag>
        <span className="mu mono">{check.durationMs} ms</span>
      </div>
      <div className="reason" data-testid="check-reason">
        {check.reason}
      </div>
      {check.evidence.map((e, i) => (
        <div className="evidence mono" key={i}>
          <span className="mu">{e.source}</span>
          {e.path && <span> path={e.path}</span>}
          {e.exitCode !== undefined && <span> exit={e.exitCode}</span>}
          {e.stdout && <pre>{e.stdout}</pre>}
          {e.stderr && <pre>{e.stderr}</pre>}
          {e.omitted ? <span className="mu"> +{e.omitted} omitted</span> : null}
        </div>
      ))}
    </div>
  )
}

export function ApprovalBox({ runId, nodeRunId, prompt, context, disabled = false }: { runId: string; nodeRunId: string; prompt: string; context: { name: string; value: string }[]; disabled?: boolean }) {
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
      <p>{prompt || t('approval.defaultPrompt')}</p>
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
export function NodePanel({ run, nodeId, lost = false, kind }: { run: RunView; nodeId: string | null; lost?: boolean; kind?: string }) {
  const { t } = useI18n()
  const [tab, setTab] = useState<'attempts' | 'log'>('attempts')
  const [chosen, setChosen] = useState<string | null>(null)
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
  return (
    <Card title={n.id} subtitle={kind ? <Tag>{kind}</Tag> : undefined} action={<NodePill status={status} />} testId="node-panel">
      {n.failureKind && (
        <div className="err">
          {n.failureKind}
          {n.diagnosticCodes.length > 0 && ` · ${n.diagnosticCodes.join(', ')}`}
        </div>
      )}
      {status === 'waiting_human' && n.approval && run.runId && n.nodeRunId && (
        <ApprovalBox runId={run.runId} nodeRunId={n.nodeRunId} prompt={n.approval.prompt} context={n.approval.context} disabled={lost} />
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
        value={tab}
        onChange={setTab}
        segments={[
          { value: 'attempts', label: n.attempts.length ? t('node.attemptsCount', { count: n.attempts.length }) : t('node.attempts'), testId: 'tab-attempts' },
          { value: 'log', label: t('node.log'), testId: 'tab-log' },
        ]}
      />
      {tab === 'attempts' &&
        (n.attempts.length === 0 || !shown ? (
          <div className="mu">{t('node.noLoop')}</div>
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
              <Evidence attempt={shown} />
              {shown.log && <pre className="log small">{shown.log}</pre>}
            </div>
          </>
        ))}
      {tab === 'log' && <pre className="log" data-testid="node-log" tabIndex={0}>{n.log || t('node.noLog')}</pre>}
    </Card>
  )
}
