import { useState } from 'react'
import { decide } from './api'
import { AttemptCompare } from './AttemptCompare'
import { CheckResultView } from './check'
import { canCompare, defaultPair, notObserved } from './compare'
import { DataTab } from './DataTab'
import { nodeData } from './dataModel'
import { ActivityView, AttemptInputView, ChangedFilesBlock, FactsHeader, nodeNotObserved } from './nodeDetail'
import { useI18n } from './i18n/context'
import { displayStatus, nodeView, type RunView } from './reducer'
import { ApprovalPill, NodePill, OutcomePill } from './status'
import type { GraphSummary } from './types'
import { Button, Card, KeyValue, SegmentedControl, Tag } from './ui/primitives'
import { initialValues, type RunValues } from './values'

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
export function NodePanel({ run, nodeId, lost = false, kind, values, graph = null, onOpenSettings }: { run: RunView; nodeId: string | null; lost?: boolean; kind?: string; values?: RunValues; graph?: GraphSummary | null; onOpenSettings?: () => void }) {
  const { t } = useI18n()
  const [tab, setTab] = useState<'attempts' | 'compare' | 'activity' | 'data' | 'log'>('attempts')
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
      <FactsHeader n={n} />
      <SegmentedControl
        label={t('node.view')}
        value={view}
        onChange={setTab}
        segments={[
          { value: 'attempts', label: n.attempts.length ? t('node.attemptsCount', { count: n.attempts.length }) : t('node.attempts'), testId: 'tab-attempts' },
          ...(comparable ? [{ value: 'compare' as const, label: t('compare.tab'), testId: 'tab-compare' }] : []),
          { value: 'activity', label: t('node.activity'), testId: 'tab-activity' },
          { value: 'data', label: t('node.data'), testId: 'tab-data' },
          { value: 'log', label: t('node.log'), testId: 'tab-log' },
        ]}
      />
      {view === 'compare' && compared && <AttemptCompare run={run} node={n} kind={kind} from={compared.from} to={compared.to} onChange={setPair} />}
      {view === 'attempts' &&
        (n.attempts.length === 0 || !shown ? (
          <>
            {n.check ? <CheckResultView check={n.check} /> : <div className="mu">{t('node.noLoop')}</div>}
            <ChangedFilesBlock files={n.changedFiles} reason={nodeNotObserved(n, kind, run.status === 'running')} check={n.check} activity={n.activity} />
          </>
        ) : (
          <>
            {n.attempts.length > 1 && (
              <SegmentedControl
                label={t('node.attemptsOf')}
                value={shown.id}
                onChange={setChosen}
                segments={n.attempts.map((a) => ({ value: a.id, label: a.outcome ? t('node.attemptTabOutcome', { n: a.number, outcome: a.outcome }) : t('node.attemptTabRunning', { n: a.number }), testId: `attempt-tab-${a.number}` }))}
              />
            )}
            <div className="attempt" key={shown.id} data-testid="attempt">
              <div className="row between">
                <b>{t('node.attempt', { n: shown.number, max: shown.max })}</b>
                <OutcomePill outcome={shown.outcome} />
              </div>
              <AttemptInputView input={shown.input} />
              {shown.check && <CheckResultView check={shown.check} />}
              <ChangedFilesBlock files={shown.changedFiles} reason={notObserved(shown, kind, run.status === 'running')} check={shown.check} activity={shown.activity} />
              {shown.log && <pre className="log small">{shown.log}</pre>}
            </div>
          </>
        ))}
      {view === 'activity' && <ActivityView run={run} n={n} />}
      {view === 'data' && <DataTab data={nodeData(values ?? initialValues(run.runId ?? ''), graph, n.id)} kind={kind} onOpenSettings={onOpenSettings} />}
      {view === 'log' && (
        <>
          {n.logDropped > 0 && <div className="mu" data-testid="log-dropped">{t('node.logDropped', { count: n.logDropped })}</div>}
          <pre className="log" data-testid="node-log" tabIndex={0}>{n.log || t('node.noLog')}</pre>
        </>
      )}
    </Card>
  )
}
