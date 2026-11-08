import { useEffect, useState } from 'react'
import type {
  EvidenceDescriptor, EvidenceDescriptorPage, HistoryDetail, HistoryEventPage,
  HistoryListPage, HistoryOutcomePage, HistoryRunSummary
} from '../preload/api'
import { adaptiveHistoryPage, attemptReferences, checkKindName, decodeEvidence, evidencePreview, evidenceScopeLabel, historyCompleteness, historyRowIncomplete, orderedHistoricalTrace, projectionPreview, verdictName } from './historyData'

function dateLabel(value: string): string {
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? value : new Date(timestamp).toLocaleString()
}

function shortID(id: string): string { return id.length > 22 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id }

export function HistoryRows({ page, loading, error, onOpen, onMore }: {
  page: HistoryListPage | null; loading: boolean; error: string | null
  onOpen: (runID: string) => void; onMore: () => void
}): React.JSX.Element {
  return <div className="history-list">
    <div className="table-head history-grid"><span>RUN</span><span>GRAPH</span><span>STORAGE</span><span>STARTED</span></div>
    {page?.runs.map((run: HistoryRunSummary) => <button type="button" className="data-row history-grid history-row" key={run.run_id} onClick={() => onOpen(run.run_id)} aria-label={`Inspect run ${run.run_id}`}>
      <span className="mono">{shortID(run.run_id)}</span><span>{run.graph_id}</span><span className={`badge badge-${!historyRowIncomplete(run) ? 'good' : 'warn'}`}>{run.storage_state}{historyRowIncomplete(run) && run.storage_state === 'complete' ? ' · partial' : ''}</span><span className="muted">{dateLabel(run.started_at)}</span>
    </button>)}
    {error ? <div className="empty-row" role="status">History unavailable: {error}</div>
      : loading && !page ? <div className="empty-row">Loading local history…</div>
        : !page?.runs.length ? <div className="empty-row">No local run history</div> : null}
    {page?.next_cursor && <div className="history-more"><button type="button" className="small-button" disabled={loading} onClick={onMore}>Load more runs</button></div>}
  </div>
}

function ReadOnlyProjection({ title, value }: { title: string; value: unknown }): React.JSX.Element {
  const preview = projectionPreview(value)
  return <details className="history-projection"><summary>{title}</summary><pre>{preview.text}</pre>{preview.truncated && <p>Preview limited to 24,000 characters. The stored projection is larger.</p>}</details>
}

function EvidenceRow({ descriptor }: { descriptor: EvidenceDescriptor }): React.JSX.Element {
  const [chunks, setChunks] = useState<Uint8Array[]>([])
  const [offset, setOffset] = useState(0)
  const [end, setEnd] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const available = descriptor.availability === 'available'
  async function load(): Promise<void> {
    if (!available || busy || end) return
    setBusy(true)
    try {
      const next = await window.agentplane.evidenceChunk(descriptor.evidence_id, offset, 1_048_576)
      if (next.availability !== 'available' || typeof next.bytes_base64 !== 'string' ||
          next.next_offset < offset || (!next.end && next.next_offset === offset) || next.total_length > (10 << 20)) {
        throw new Error('Evidence chunk is unavailable')
      }
      const bytes = decodeEvidence(next.bytes_base64)
      if (next.next_offset !== offset + bytes.length) throw new Error('Evidence chunk offset changed')
      setChunks((items) => [...items, bytes])
      setOffset(next.next_offset)
      setEnd(next.end)
      setError(null)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Evidence unavailable') }
    finally { setBusy(false) }
  }
  let preview = { text: '', truncated: false, binary: false }
  if (chunks.length) {
    const data = new Uint8Array(chunks.reduce((total, chunk) => total + chunk.length, 0))
    let position = 0
    for (const chunk of chunks) { data.set(chunk, position); position += chunk.length }
    preview = evidencePreview(data)
  }
  return <div className="history-evidence-row">
    <div><strong>{descriptor.kind}</strong><span className="muted">{evidenceScopeLabel(descriptor)}</span>{descriptor.source_path && <span className="mono">{descriptor.source_path}</span>}{descriptor.capture_group && <span className="muted">Capture {shortID(descriptor.capture_group)}</span>}<span className="mono">{shortID(descriptor.evidence_id)}</span><span className="muted">{available ? `${descriptor.byte_length ?? 0} bytes` : descriptor.omission_reason ?? 'Unavailable'}</span>{descriptor.redacted && <span className="badge badge-warn">Sanitized · partial</span>}</div>
    {available && <button type="button" className="small-button" disabled={busy || end} onClick={() => void load()}>{chunks.length ? end ? 'Loaded' : 'Load next chunk' : 'Read evidence'}</button>}
    {error && <p role="alert">{error}</p>}
    {chunks.length > 0 && <>{preview.binary && <p>Binary evidence · hex preview</p>}<pre>{preview.text}</pre></>}
    {preview.truncated && <p>Preview limited to 24,000 characters.</p>}
  </div>
}

export function HistoryDetailView({ runID, onClose }: { runID: string; onClose: () => void }): React.JSX.Element {
  const [detail, setDetail] = useState<HistoryDetail | null>(null)
  const [outcomes, setOutcomes] = useState<HistoryOutcomePage | null>(null)
  const [events, setEvents] = useState<HistoryEventPage | null>(null)
  const [evidence, setEvidence] = useState<EvidenceDescriptorPage | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [reference, setReference] = useState<{ sequence: number; value?: unknown; omission?: string; error?: string } | null>(null)
  useEffect(() => {
    let alive = true
    setDetail(null); setOutcomes(null); setEvents(null); setEvidence(null); setErrors({}); setReference(null)
    const operations = [
      ['run', window.agentplane.getHistory(runID), setDetail],
      ['outcomes', adaptiveHistoryPage((limit) => window.agentplane.outcomesHistory(runID, limit), 32), setOutcomes],
      ['events', adaptiveHistoryPage((limit) => window.agentplane.eventsHistory(runID, 0, limit), 64), setEvents],
      ['evidence', window.agentplane.listEvidence(runID, 64), setEvidence]
    ] as const
    for (const [name, promise, update] of operations) {
      void promise.then((value) => { if (alive) (update as (value: never) => void)(value as never) })
        .catch((cause) => { if (alive) setErrors((old) => ({ ...old, [name]: cause instanceof Error ? cause.message : 'Unavailable' })) })
    }
    return () => { alive = false }
  }, [runID])

  async function moreOutcomes(): Promise<void> {
    if (!outcomes || busy) return
    setBusy(true)
    try {
      const next = await adaptiveHistoryPage((limit) => window.agentplane.outcomesHistory(runID, limit, outcomes.next_node_cursor, outcomes.next_attempt_cursor), 32)
      setOutcomes({ nodes: [...outcomes.nodes, ...next.nodes], attempts: [...outcomes.attempts, ...next.attempts],
        next_node_cursor: next.next_node_cursor, next_attempt_cursor: next.next_attempt_cursor })
    } catch (cause) { setErrors((old) => ({ ...old, outcomes: cause instanceof Error ? cause.message : 'Unavailable' })) }
    finally { setBusy(false) }
  }
  async function moreEvents(): Promise<void> {
    if (!events || busy || events.next_sequence >= events.watermark) return
    setBusy(true)
    try {
      const next = await adaptiveHistoryPage((limit) => window.agentplane.eventsHistory(runID, events.next_sequence, limit), 64)
      if (next.next_sequence <= events.next_sequence) throw new Error('History cursor did not advance')
      setEvents({ ...next, events: [...events.events, ...next.events], omissions: [...events.omissions, ...next.omissions] })
    } catch (cause) { setErrors((old) => ({ ...old, events: cause instanceof Error ? cause.message : 'Unavailable' })) }
    finally { setBusy(false) }
  }
  async function moreEvidence(): Promise<void> {
    if (!evidence?.next_cursor || busy) return
    setBusy(true)
    try {
      const next = await window.agentplane.listEvidence(runID, 64, undefined, evidence.next_cursor)
      setEvidence({ ...next, descriptors: [...evidence.descriptors, ...next.descriptors] })
    } catch (cause) {
      if (cause instanceof Error && 'code' in cause && cause.code === 'stale_cursor') {
        try { setEvidence(await window.agentplane.listEvidence(runID, 64)); setErrors((old) => { const copy = { ...old }; delete copy.evidence; return copy }) }
        catch (retryCause) { setErrors((old) => ({ ...old, evidence: retryCause instanceof Error ? retryCause.message : 'Unavailable' })) }
      } else setErrors((old) => ({ ...old, evidence: cause instanceof Error ? cause.message : 'Unavailable' }))
    }
    finally { setBusy(false) }
  }
  async function togglePin(): Promise<void> {
    if (!detail || busy) return
    setBusy(true)
    try {
      const next = await window.agentplane.pinHistory(runID, detail.run.revision, !detail.run.pinned)
      setDetail({ ...detail, run: { ...detail.run, revision: next.revision, pinned: next.pinned } })
      setErrors((old) => { const copy = { ...old }; delete copy.pin; return copy })
    } catch (cause) { setErrors((old) => ({ ...old, pin: cause instanceof Error ? cause.message : 'Pin unavailable' })) }
    finally { setBusy(false) }
  }
  async function inspectReference(sequence: number): Promise<void> {
    setReference({ sequence })
    try {
      const page = await window.agentplane.eventsHistory(runID, sequence - 1, 1)
      const event = page.events.find((item) => item.sequence === sequence)
      const omission = page.omissions.find((item) => item.from <= sequence && sequence <= item.to)
      setReference({ sequence, value: event?.projection, omission: omission?.reason ?? (event ? undefined : 'not available') })
    } catch (cause) { setReference({ sequence, error: cause instanceof Error ? cause.message : 'Unavailable' }) }
  }
  const omitted = detail ? historyCompleteness(detail) : []
  return <section className="panel history-detail" aria-label="Historical run detail">
    <div className="panel-heading"><h2>Historical run · {shortID(runID)}</h2><div className="history-detail-actions">{detail && <button type="button" className="small-button" disabled={busy} onClick={() => void togglePin()}>{detail.run.pinned ? 'Unpin run' : 'Pin run'}</button>}<button type="button" className="small-button" onClick={onClose}>Close detail</button></div></div>
    {errors.pin && <div className="history-incomplete" role="alert">Pin unavailable: {errors.pin}</div>}
    {errors.run ? <div className="empty-row" role="alert">Run detail unavailable: {errors.run}</div> : !detail ? <div className="empty-row">Loading saved run…</div> : <>
      <div className="history-facts"><span>Graph <strong>{detail.run.graph_id}</strong></span><span>Runtime <strong>{detail.run.runtime_state ?? 'Unknown'}</strong></span><span>Storage <strong>{detail.run.storage_state}{detail.run.pinned ? ' · Pinned' : ''}</strong></span><span>Started <strong>{dateLabel(detail.run.started_at)}</strong></span></div>
      {detail.run.storage_state !== 'complete' && <div className="history-incomplete">This run has no completed archive. Execution cannot be resumed from history.</div>}
      {omitted.length > 0 && <div className="history-incomplete">Incomplete inspection: {omitted.join(', ')}</div>}
      <div className="history-projections"><ReadOnlyProjection title="Safe graph snapshot" value={detail.graph} /><ReadOnlyProjection title="Launch inputs" value={detail.launch} />{detail.result !== undefined && <ReadOnlyProjection title="Run result" value={detail.result} />}</div>
    </>}
    <div className="history-subsection"><h3>Node and attempt outcomes</h3>{errors.outcomes && <p role="alert">{errors.outcomes}</p>}
      {!outcomes && !errors.outcomes && <p>Loading outcomes…</p>}
      {outcomes?.nodes.map((node) => <div className="history-item" key={node.node_run_id}><strong>{node.node_id}</strong><span>{node.status}</span><small className="mono">{shortID(node.node_run_id)}</small><ReadOnlyProjection title="Node output and failure codes" value={node.projection} /></div>)}
      {outcomes?.attempts.map((attempt) => { const refs = attemptReferences(attempt); return <div className="history-item" key={attempt.attempt_id}><strong>Attempt {shortID(attempt.attempt_id)}</strong><span>{attempt.status}</span>
        <div className="history-refs">Check Result: {refs.check ? <>{refs.check.id} · {checkKindName(refs.check.kind)} · {verdictName(refs.check.verdict)} <button type="button" onClick={() => void inspectReference(refs.check!.sequence)}>View event #{refs.check.sequence}</button></> : 'Unavailable'}<br />Return Envelope: {refs.envelope ? <>{refs.envelope.unitID} · {verdictName(refs.envelope.verdict)} <button type="button" onClick={() => void inspectReference(refs.envelope!.sequence)}>View event #{refs.envelope.sequence}</button></> : 'Unavailable'}{!refs.complete && <span> · Partial references</span>}</div></div> })}
      {reference && <div className="history-reference"><strong>Event #{reference.sequence}</strong>{reference.error ? <p role="alert">{reference.error}</p> : reference.omission ? <p>Omitted · {reference.omission}</p> : reference.value !== undefined ? <ReadOnlyProjection title="Safe referenced fact" value={reference.value} /> : <p>Loading referenced fact…</p>}</div>}
      {outcomes && outcomes.nodes.length + outcomes.attempts.length === 0 && <p>No indexed outcomes</p>}
      {(outcomes?.next_node_cursor || outcomes?.next_attempt_cursor) && <button type="button" className="small-button" disabled={busy} onClick={() => void moreOutcomes()}>Load more outcomes</button>}
    </div>
    <div className="history-subsection"><h3>Retained events</h3>{errors.events && <p role="alert">{errors.events}</p>}
      {!events && !errors.events && <p>Loading events…</p>}
      {events && !events.complete && <p className="history-incomplete">{detail?.run.storage_state === 'complete' ? 'The saved event trace has omitted source sequences.' : 'The saved event trace is incomplete; execution did not finish archiving.'}</p>}
      {events && orderedHistoricalTrace(events).map((item) => item.kind === 'event'
        ? <div className="history-item" key={`event-${item.sequence}`}><span className="mono">#{item.sequence}</span><strong>{typeof item.projection === 'object' && item.projection !== null && 'type' in item.projection ? String(item.projection.type) : 'Retained fact'}</strong><ReadOnlyProjection title="Safe event" value={item.projection} /></div>
        : <div className="history-item history-omission" key={`omission-${item.sequence}`}><span className="mono">#{item.sequence}{item.to !== item.sequence ? `–${item.to}` : ''}</span><span>Omitted · {item.reason}</span></div>)}
      {events && events.next_sequence < events.watermark && <button type="button" className="small-button" disabled={busy} onClick={() => void moreEvents()}>Load more events</button>}
    </div>
    <div className="history-subsection"><h3>Managed evidence</h3>{errors.evidence && <p role="alert">{errors.evidence}</p>}
      {!evidence && !errors.evidence && <p>Loading evidence…</p>}
      {evidence && !evidence.complete && <p className="history-incomplete">Some managed evidence is unavailable or partial.</p>}
      {evidence?.descriptors.map((descriptor) => <EvidenceRow key={descriptor.evidence_id} descriptor={descriptor} />)}
      {evidence && evidence.descriptors.length === 0 && <p>No managed evidence for this run</p>}
      {evidence?.next_cursor && <button type="button" className="small-button" disabled={busy} onClick={() => void moreEvidence()}>Load more evidence</button>}
    </div>
  </section>
}
