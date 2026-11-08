// The service owns terminal truth. This reducer projects retained facts for
// display and keeps transport omissions visible without fabricating events.
export type RunFact = {
  run_id: string
  sequence: number
  type: string
  graph_id?: string
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

export type OmissionRange = {
  from: number
  to: number
  reason: 'sensitive_identity' | 'not_retained'
}

export type EventPage = {
  run_id: string
  after_sequence: number
  events: RunFact[]
  event_hashes: string[]
  omissions: OmissionRange[]
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
  history_availability?: 1 | 2 | 3
}

export type RunView = {
  runID: string
  cursor: number
  events: RunFact[]
  hashes: ReadonlyMap<number, string>
  conflicts: number[]
  watermark: number
  omitted: DeliveryOmissions
  omissionRanges: OmissionRange[]
  traceFinished: boolean
  state: string
  finished: boolean
  historyAvailability?: 1 | 2 | 3
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
    omitted: { ...zeroOmissions }, omissionRanges: [], traceFinished: false, state: 'running', finished: false }
}

export function applyPage(current: RunView, page: EventPage): RunView {
  if (page.run_id !== current.runID || !Array.isArray(page.events) ||
      !Array.isArray(page.event_hashes) || !Array.isArray(page.omissions) ||
      page.events.length !== page.event_hashes.length ||
      !safeInteger(page.after_sequence) || !safeInteger(page.next_after) ||
      !safeInteger(page.watermark) || page.after_sequence > page.watermark ||
      page.next_after < page.after_sequence || page.next_after > page.watermark) {
    throw new Error('invalid retained event page')
  }
  const hashes = new Map(current.hashes)
  const bySequence = new Map(current.events.map((event) => [event.sequence, event]))
  const conflicts = new Set(current.conflicts)
  const coverage: Array<{ from: number; to: number }> = []
  let previous = page.after_sequence
  for (let index = 0; index < page.events.length; index++) {
    const event = page.events[index]
    const hash = page.event_hashes[index]
    if (event.run_id !== current.runID || !safeInteger(event.sequence) ||
        event.sequence <= previous || event.sequence > page.next_after ||
        !/^sha256:[0-9a-f]{64}$/.test(hash)) {
      throw new Error('invalid retained event identity')
    }
    previous = event.sequence
    coverage.push({ from: event.sequence, to: event.sequence })
    if (current.omissionRanges.some((range) => range.from <= event.sequence && event.sequence <= range.to)) {
      conflicts.add(event.sequence)
      continue
    }
    const acceptedHash = hashes.get(event.sequence)
    if (acceptedHash && acceptedHash !== hash) {
      conflicts.add(event.sequence)
    } else if (!acceptedHash) {
      hashes.set(event.sequence, hash)
      bySequence.set(event.sequence, event)
    }
  }
  for (const omission of page.omissions) {
    if (!safeInteger(omission.from) || !safeInteger(omission.to) ||
        omission.from <= page.after_sequence || omission.from > omission.to ||
        omission.to > page.next_after ||
        !['sensitive_identity', 'not_retained'].includes(omission.reason)) {
      throw new Error('invalid retained event omission')
    }
    coverage.push({ from: omission.from, to: omission.to })
    for (const accepted of current.events) {
      if (omission.from <= accepted.sequence && accepted.sequence <= omission.to) {
        conflicts.add(accepted.sequence)
        if (omission.reason === 'sensitive_identity') {
          bySequence.delete(accepted.sequence)
          hashes.delete(accepted.sequence)
        }
      }
    }
  }
  coverage.sort((a, b) => a.from - b.from)
  let covered = page.after_sequence
  for (const item of coverage) {
    if (item.from !== covered + 1) throw new Error('unexplained retained event sequence')
    covered = item.to
  }
  if (covered !== page.next_after) throw new Error('unexplained retained event sequence')
  if (page.has_more && page.next_after === page.after_sequence) {
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
    omitted: { ...page.stats },
    omissionRanges: [...new Map([...current.omissionRanges, ...page.omissions].map(
      (range) => [range.from + ':' + range.to + ':' + range.reason, range])).values()]
      .sort((a, b) => a.from - b.from),
    traceFinished: current.traceFinished || page.finished }
}

export function applySnapshot(current: RunView, snapshot: RunSnapshot): RunView {
  if (snapshot.run_id !== current.runID || !safeInteger(snapshot.trace_watermark) ||
      typeof snapshot.state !== 'string' || !snapshot.state ||
      (snapshot.history_availability !== undefined &&
        ![1, 2, 3].includes(snapshot.history_availability))) {
    throw new Error('invalid run snapshot')
  }
  return { ...current, state: snapshot.state, finished: snapshot.finished,
    historyAvailability: snapshot.history_availability ?? current.historyAvailability,
    watermark: Math.max(current.watermark, snapshot.trace_watermark) }
}

// A replay from the previous run must never label a newly selected run.
export function currentRunView(view: RunView | null, runID?: string): RunView | null {
  return runID && view?.runID === runID ? view : null
}
