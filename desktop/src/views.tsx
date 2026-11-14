import { useState } from 'react'
import { listApprovals, listRuns, startRun, validateGraph, type ApiFailure } from './api'
import { GraphMap } from './GraphMap'
import { useRunStream, usePoll } from './hooks'
import { ApprovalBox, NodePanel } from './NodePanel'
import { nodeView, displayStatus } from './reducer'
import { Pill, RunPill } from './status'
import type { Diagnostic } from './types'

export function Dashboard({ onOpen }: { onOpen: (id: string) => void }) {
  const runs = usePoll(listRuns, [], 1500)
  return (
    <section>
      <h1>Runs</h1>
      {runs.length === 0 ? (
        <div className="empty-state" data-testid="no-runs">
          No runs yet. Start one from “New run”.
        </div>
      ) : (
        <table className="runs" data-testid="runs-table">
          <thead>
            <tr>
              <th>Status</th>
              <th>Graph</th>
              <th>Started</th>
              <th>Events</th>
              <th>Run</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => (
              <tr key={r.run_id} onClick={() => onOpen(r.run_id)} data-testid="run-row">
                <td>
                  <RunPill status={r.status} />
                </td>
                <td>{r.graph_id}</td>
                <td className="mono">{new Date(r.started_at).toLocaleString()}</td>
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

export function RunInspector({ runId, onBack }: { runId: string; onBack: () => void }) {
  const { run, graph, yaml, following, notFound } = useRunStream(runId)
  const [selected, setSelected] = useState<string | null>(null)
  const [showYaml, setShowYaml] = useState(false)
  if (notFound) return <section><button onClick={onBack}>← Runs</button><div className="empty-state">This run is not in the local store.</div></section>
  const waiting = run.order.filter((id) => displayStatus(nodeView(run, id)) === 'waiting_human')
  const focus = selected ?? waiting[0] ?? run.order.find((id) => nodeView(run, id).status === 'failed') ?? null
  return (
    <section className="inspector">
      <div className="row between">
        <div className="row">
          <button onClick={onBack}>← Runs</button>
          <h1>{run.graphId ?? 'Run'}</h1>
          <RunPill status={run.status} />
          {following && run.status === 'running' && <span className="mu">live</span>}
          {run.failureKind && <span className="tag">{run.failureKind}</span>}
        </div>
        <div className="row">
          <span className="mono mu" data-testid="event-count">{run.eventCount} events</span>
          <button onClick={() => setShowYaml((v) => !v)}>{showYaml ? 'Map' : 'YAML'}</button>
        </div>
      </div>
      <div className="split">
        <div className="main">
          {showYaml ? (
            <pre className="log yaml" data-testid="graph-yaml">{yaml}</pre>
          ) : graph ? (
            <GraphMap graph={graph} run={run} selected={focus} onSelect={setSelected} />
          ) : (
            <div className="empty-state">Loading graph…</div>
          )}
        </div>
        <NodePanel run={run} nodeId={focus} />
      </div>
    </section>
  )
}

export function Approvals({ onOpen }: { onOpen: (runId: string) => void }) {
  const approvals = usePoll(listApprovals, [], 1000)
  return (
    <section>
      <h1>Approvals</h1>
      {approvals.length === 0 ? (
        <div className="empty-state" data-testid="no-approvals">
          Nothing is waiting for a decision.
        </div>
      ) : (
        approvals.map((a) => (
          <div className="card" key={a.node_run_id} data-testid="approval-card">
            <div className="row between">
              <b>{a.node_id}</b>
              <button onClick={() => onOpen(a.run_id)}>Open run</button>
            </div>
            <ApprovalBox runId={a.run_id} nodeRunId={a.node_run_id} prompt={a.prompt} context={a.context} />
          </div>
        ))
      )}
    </section>
  )
}

export function RunConfig({ onStarted }: { onStarted: (runId: string) => void }) {
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
      <h1>New run</h1>
      <label className="lb" htmlFor="graph-path">Graph file</label>
      <div className="row">
        <input id="graph-path" data-testid="graph-path" value={path} placeholder="/path/to/graph.yaml" onChange={(e) => { setPath(e.target.value); setResult(null) }} />
        <button onClick={choose}>Choose…</button>
        <button data-testid="validate" disabled={!path} onClick={() => validate()}>Validate</button>
      </div>
      <label className="lb" htmlFor="inputs">Inputs (key=value, one per line)</label>
      <textarea id="inputs" value={inputs} onChange={(e) => setInputs(e.target.value)} />
      <div className="row">
        <button className="primary" data-testid="run" disabled={blocked || busy} onClick={run}>Run</button>
        {result && <Pill tone={result.status === 'valid' ? 'p' : 'x'} testId="preflight-status">{result.status === 'valid' ? 'Ready' : 'Blocked'}</Pill>}
      </div>
      {error && <div className="err">{error.message}</div>}
      {diagnostics.map((d, i) => (
        <div className={`diag ${d.severity}`} key={i}>
          <b>{d.code}</b> {d.message}
          {d.location?.node_id && <span className="tag">{d.location.node_id}</span>}
          {d.suggestion && <div className="mu">{d.suggestion}</div>}
        </div>
      ))}
    </section>
  )
}
