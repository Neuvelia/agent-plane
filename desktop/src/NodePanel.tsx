import { useState } from 'react'
import { decide } from './api'
import { displayStatus, nodeView, type Attempt, type RunView } from './reducer'
import { NodePill, OutcomePill, VerdictPill } from './status'

function Evidence({ attempt }: { attempt: Attempt }) {
  const check = attempt.check
  if (!check) return null
  return (
    <div className="check" data-testid="check-result">
      <div className="row">
        <VerdictPill verdict={check.verdict} />
        <span className="mono">{check.checkId}</span>
        <span className="tag">{check.kind}</span>
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

export function ApprovalBox({ runId, nodeRunId, prompt, context }: { runId: string; nodeRunId: string; prompt: string; context: { name: string; value: string }[] }) {
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
      <div className="lb">Approval needed</div>
      <p>{prompt || 'A human decision is required.'}</p>
      {context.map((c) => (
        <div key={c.name} className="mono ctx">
          <span className="mu">{c.name}:</span> {c.value}
        </div>
      ))}
      <textarea aria-label="Comment" placeholder="Comment (optional)" value={comment} maxLength={2048} onChange={(e) => setComment(e.target.value)} />
      <div className="row">
        <button className="primary" disabled={busy} onClick={() => submit(true)}>
          Approve
        </button>
        <button className="danger" disabled={busy} onClick={() => submit(false)}>
          Reject
        </button>
      </div>
      {error && <div className="err">{error}</div>}
    </div>
  )
}

export function NodePanel({ run, nodeId }: { run: RunView; nodeId: string | null }) {
  const [tab, setTab] = useState<'attempts' | 'log'>('attempts')
  if (!nodeId) return <aside className="panel empty">Select a node to see its attempts, checks and log.</aside>
  const n = nodeView(run, nodeId)
  const status = displayStatus(n)
  return (
    <aside className="panel" data-testid="node-panel">
      <div className="row between">
        <h2>{n.id}</h2>
        <NodePill status={status} />
      </div>
      {n.failureKind && (
        <div className="err">
          {n.failureKind}
          {n.diagnosticCodes.length > 0 && ` · ${n.diagnosticCodes.join(', ')}`}
        </div>
      )}
      {status === 'waiting_human' && n.approval && run.runId && n.nodeRunId && (
        <ApprovalBox runId={run.runId} nodeRunId={n.nodeRunId} prompt={n.approval.prompt} context={n.approval.context} />
      )}
      {n.approval?.decision && (
        <div className="decision" data-testid="decision">
          Decision: <b>{n.approval.decision}</b>
          {n.approval.comment && <span className="mu"> — {n.approval.comment}</span>}
        </div>
      )}
      <div className="tabs">
        <button className={tab === 'attempts' ? 'on' : ''} onClick={() => setTab('attempts')}>
          Attempts{n.attempts.length ? ` (${n.attempts.length})` : ''}
        </button>
        <button className={tab === 'log' ? 'on' : ''} onClick={() => setTab('log')}>
          Log
        </button>
      </div>
      {tab === 'attempts' &&
        (n.attempts.length === 0 ? (
          <div className="mu">This node has no correction loop, so it has a single execution.</div>
        ) : (
          n.attempts.map((a) => (
            <div className="attempt" key={a.id} data-testid="attempt">
              <div className="row between">
                <b>
                  Attempt {a.number}/{a.max}
                </b>
                <OutcomePill outcome={a.outcome} />
              </div>
              <Evidence attempt={a} />
              {a.log && <pre className="log small">{a.log}</pre>}
            </div>
          ))
        ))}
      {tab === 'log' && <pre className="log" data-testid="node-log">{n.log || 'No output yet.'}</pre>}
    </aside>
  )
}
