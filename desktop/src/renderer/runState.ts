// The service owns terminal truth. This reducer projects retained facts for
// display and keeps transport omissions visible without fabricating events.
export type RunFact = {
  run_id: string
  sequence: number
  type: string
  timestamp: string
  event_schema_version: number
  payload: unknown
  [key: string]: unknown
}

export type DeliveryOmissions = {
  retained_omitted_events: number
  retained_omitted_bytes: number
  live_omitted_events: number
  live_omitted_bytes: number
  live_stopped: boolean
}

export type EventPage = {
  run_id: string
  after_sequence: number
  events: RunFact[]
  event_hashes: string[]
  next_after: number
  has_more: boolean
  watermark: number
  stats: DeliveryOmissions
  finished: boolean
}

export type RunSnapshot = {
  run_id: string
  state: string
  finished: boolean
  trace_watermark: number
}

export type RunView = {
  runID: string
  cursor: number
  events: RunFact[]
  hashes: ReadonlyMap<number, string>
  conflicts: number[]
  watermark: number
  omitted: DeliveryOmissions
  traceFinished: boolean
  state: string
  finished: boolean
}

const zeroOmissions: DeliveryOmissions = {
  retained_omitted_events: 0, retained_omitted_bytes: 0,
  live_omitted_events: 0, live_omitted_bytes: 0, live_stopped: false
}

function safeInteger(value: number): boolean {
  return Number.isSafeInteger(value) && value >= 0
}

export function emptyRunView(runID: string): RunView {
  if (!runID) throw new Error('missing run identity')
  return { runID, cursor: 0, events: [], hashes: new Map(), conflicts: [], watermark: 0,
    omitted: { ...zeroOmissions }, traceFinished: false, state: 'running', finished: false }
}

export function applyPage(current: RunView, page: EventPage): RunView {
  if (page.run_id !== current.runID || !Array.isArray(page.events) ||
      !Array.isArray(page.event_hashes) || page.events.length !== page.event_hashes.length ||
      !safeInteger(page.after_sequence) || !safeInteger(page.next_after) ||
      !safeInteger(page.watermark) || page.after_sequence > page.watermark ||
      page.next_after < page.after_sequence || page.next_after > page.watermark) {
    throw new Error('invalid retained event page')
  }
  const hashes = new Map(current.hashes)
  const bySequence = new Map(current.events.map((event) => [event.sequence, event]))
  const conflicts = new Set(current.conflicts)
  let previous = page.after_sequence
  for (let index = 0; index < page.events.length; index++) {
    const event = page.events[index]
    const hash = page.event_hashes[index]
    if (event.run_id !== current.runID || !safeInteger(event.sequence) ||
        event.sequence <= previous || event.sequence > page.watermark ||
        !/^sha256:[0-9a-f]{64}$/.test(hash)) {
      throw new Error('invalid retained event identity')
    }
    previous = event.sequence
    const acceptedHash = hashes.get(event.sequence)
    if (acceptedHash && acceptedHash !== hash) {
      conflicts.add(event.sequence)
    } else if (!acceptedHash) {
      hashes.set(event.sequence, hash)
      bySequence.set(event.sequence, event)
    }
  }
  if (page.has_more && (page.events.length === 0 || page.next_after !== previous)) {
    throw new Error('invalid retained event continuation')
  }
  if (!page.has_more && page.next_after !== page.watermark) {
    throw new Error('invalid retained event watermark')
  }
  for (const value of [page.stats.retained_omitted_events, page.stats.retained_omitted_bytes,
    page.stats.live_omitted_events, page.stats.live_omitted_bytes]) {
    if (!safeInteger(value)) throw new Error('invalid omission count')
  }
  return { ...current, hashes,
    cursor: Math.max(current.cursor, page.next_after),
    events: [...bySequence.values()].sort((a, b) => a.sequence - b.sequence),
    conflicts: [...conflicts].sort((a, b) => a - b),
    watermark: Math.max(current.watermark, page.watermark),
    omitted: { ...page.stats }, traceFinished: current.traceFinished || page.finished }
}

export function applySnapshot(current: RunView, snapshot: RunSnapshot): RunView {
  if (snapshot.run_id !== current.runID || !safeInteger(snapshot.trace_watermark) ||
      typeof snapshot.state !== 'string' || !snapshot.state) {
    throw new Error('invalid run snapshot')
  }
  return { ...current, state: snapshot.state, finished: snapshot.finished,
    watermark: Math.max(current.watermark, snapshot.trace_watermark) }
}
