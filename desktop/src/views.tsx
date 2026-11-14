import { useEffect, useState } from 'react'
import { listApprovals, listRuns, startRun, validateGraph, type ApiFailure } from './api'
import { GraphMap } from './GraphMap'
import { useRunStream, usePoll } from './hooks'
import { formatDateTime } from './i18n'
import { useI18n } from './i18n/context'
import { ApprovalBox, NodePanel } from './NodePanel'
import { nodeView, displayStatus } from './reducer'
import { RunPill } from './status'
import type { Diagnostic } from './types'
import { CodeView } from './ui/data'
import { Button, Field, Pill, Tag } from './ui/primitives'

// R0 re-hosts the F2.5 screens in the new shell. Their behaviour is unchanged;
// R1 redesigns them.

export function Dashboard({ onOpen }: { onOpen: (id: string) => void }) {
  const { t, locale } = useI18n()
  const runs = usePoll(listRuns, [], 1500)
  return (
    <section>
      <h1>{t('runs.title')}</h1>
      {runs.length === 0 ? (
        <div className="empty-state" data-testid="no-runs">
          {t('runs.empty')}
        </div>
      ) : (
        <table className="runs" data-testid="runs-table">
          <thead>
            <tr>
              <th>{t('runs.col.status')}</th>
              <th>{t('runs.col.graph')}</th>
              <th>{t('runs.col.started')}</th>
              <th>{t('runs.col.events')}</th>
              <th>{t('runs.col.run')}</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => (
              <tr key={r.run_id} onClick={() => onOpen(r.run_id)} data-testid="run-row">
                <td>
                  <RunPill status={r.status} />
                </td>
                <td>{r.graph_id}</td>
                <td className="mono">{formatDateTime(locale, r.started_at)}</td>
                <td className="mono">{r.event_count}</td>
                <td className="mono mu">{r.run_id.slice(0, 8)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

export function RunInspector({ runId, onBack, onLost }: { runId: string; onBack: () => void; onLost?: (lost: boolean) => void }) {
  const { t } = useI18n()
  const { run, graph, yaml, following, notFound, lost, reconnect } = useRunStream(runId)
  const [selected, setSelected] = useState<string | null>(null)
  const [showYaml, setShowYaml] = useState(false)
  const isLost = lost !== null
  useEffect(() => {
    onLost?.(isLost)
    return () => onLost?.(false)
  }, [isLost, onLost])
  if (notFound)
    return (
      <section>
        <Button onClick={onBack}>{t('run.back')}</Button>
        <div className="empty-state">{t('run.notFound')}</div>
      </section>
    )
  const waiting = run.order.filter((id) => displayStatus(nodeView(run, id)) === 'waiting_human')
  const focus = selected ?? waiting[0] ?? run.order.find((id) => nodeView(run, id).status === 'failed') ?? null
  return (
    <section className="inspector">
      <div className="row between">
        <div className="row">
          <Button onClick={onBack}>{t('run.back')}</Button>
          <h1>{run.graphId ?? 'Run'}</h1>
          {lost ? <Pill tone="x" testId="run-status">{t('status.connectionLost')}</Pill> : <RunPill status={run.status} />}
          {following && run.status === 'running' && <span className="mu">{t('status.live')}</span>}
          {run.failureKind && <Tag>{run.failureKind}</Tag>}
        </div>
        <div className="row">
          <span className="mono mu" data-testid="event-count">{t('run.events', { count: run.eventCount })}</span>
          <Button onClick={() => setShowYaml((v) => !v)}>{showYaml ? t('run.mapView') : t('run.yamlView')}</Button>
        </div>
      </div>
      {lost && (
        <div className="banner" role="alert" data-testid="connection-lost">
          <span>{t('run.lostBanner', { message: lost })}</span>
          <Button onClick={reconnect}>{t('run.reconnect')}</Button>
        </div>
      )}
      <div className="split">
        <div className="main">
          {showYaml ? (
            <CodeView text={yaml} testId="graph-yaml" />
          ) : graph ? (
            <GraphMap graph={graph} run={run} selected={focus} onSelect={setSelected} />
          ) : (
            <div className="empty-state">{t('run.loadingGraph')}</div>
          )}
        </div>
        <NodePanel run={run} nodeId={focus} lost={lost !== null} />
      </div>
    </section>
  )
}

export function Approvals({ onOpen }: { onOpen: (runId: string) => void }) {
  const { t } = useI18n()
  const approvals = usePoll(listApprovals, [], 1000)
  return (
    <section>
      <h1>{t('approvals.title')}</h1>
      {approvals.length === 0 ? (
        <div className="empty-state" data-testid="no-approvals">
          {t('approvals.empty')}
        </div>
      ) : (
        approvals.map((a) => (
          <div className="card" key={a.node_run_id} data-testid="approval-card">
            <div className="row between">
              <b>{a.node_id}</b>
              <Button onClick={() => onOpen(a.run_id)}>{t('approval.openRun')}</Button>
            </div>
            <ApprovalBox runId={a.run_id} nodeRunId={a.node_run_id} prompt={a.prompt} context={a.context} />
          </div>
        ))
      )}
    </section>
  )
}

export function RunConfig({ onStarted }: { onStarted: (runId: string) => void }) {
  const { t } = useI18n()
  const [path, setPath] = useState('')
  const [inputs, setInputs] = useState('')
  const [result, setResult] = useState<{ status: string; diagnostics: Diagnostic[] } | null>(null)
  const [error, setError] = useState<ApiFailure | null>(null)
  const [busy, setBusy] = useState(false)

  async function validate(p = path) {
    setError(null)
    setResult(null)
    const r = await validateGraph(p)
    if ('message' in r) setError(r)
    else setResult(r)
  }
  async function choose() {
    const picked = await window.agentplane.pickGraph()
    if (picked) {
      setPath(picked)
      void validate(picked)
    }
  }
  async function run() {
    setBusy(true)
    setError(null)
    const lines = inputs.split('\n').map((l) => l.trim()).filter(Boolean)
    const r = await startRun(path, lines)
    setBusy(false)
    if ('message' in r) setError(r)
    else onStarted(r.runId)
  }
  const blocked = !result || result.status !== 'valid'
  const diagnostics = [...(result?.diagnostics ?? []), ...(error?.diagnostics ?? [])]
  return (
    <section className="config">
      <h1>{t('config.title')}</h1>
      <Field label={t('config.graphFile')} htmlFor="graph-path">
        <div className="row">
          <input id="graph-path" data-testid="graph-path" value={path} placeholder="/path/to/graph.yaml" onChange={(e) => { setPath(e.target.value); setResult(null) }} />
          <Button data-testid="choose" onClick={choose}>{t('config.choose')}</Button>
          <Button data-testid="validate" disabled={!path} onClick={() => validate()}>{t('config.validate')}</Button>
        </div>
      </Field>
      <Field label={t('config.inputs')} htmlFor="inputs">
        <textarea id="inputs" value={inputs} onChange={(e) => setInputs(e.target.value)} />
      </Field>
      <div className="row">
        <Button variant="primary" data-testid="run" disabled={blocked || busy} onClick={run}>{t('config.run')}</Button>
        {result && <Pill tone={result.status === 'valid' ? 'p' : 'x'} testId="preflight-status">{result.status === 'valid' ? t('config.ready') : t('config.blocked')}</Pill>}
      </div>
      {error && <div className="err">{error.message}</div>}
      {diagnostics.map((d, i) => (
        <div className={`diag ${d.severity}`} key={i}>
          <b>{d.code}</b> {d.message}
          {d.location?.node_id && <Tag>{d.location.node_id}</Tag>}
          {d.suggestion && <div className="mu">{d.suggestion}</div>}
        </div>
      ))}
    </section>
  )
}
