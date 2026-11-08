import type { EvidenceDescriptor, HistoryAttemptOutcome, HistoryDetail, HistoryEventPage, HistoryRunSummary } from '../preload/api'

export function evidenceScopeLabel(descriptor: EvidenceDescriptor): string {
  if (descriptor.attempt_id) return `Attempt · ${descriptor.attempt_id}`
  if (descriptor.node_run_id) return `Node evidence · ${descriptor.node_run_id}`
  return 'Evidence scope unavailable'
}

export async function adaptiveHistoryPage<T>(load: (limit: number) => Promise<T>, preferredLimit: number): Promise<T> {
  let limit = preferredLimit
  for (;;) {
    try { return await load(limit) }
    catch (cause) {
      if (!(cause instanceof Error) || !('code' in cause) || cause.code !== 'page_too_large' || limit <= 1) throw cause
      limit = Math.max(1, Math.floor(limit / 2))
    }
  }
}

export type HistoricalTraceItem =
  | { kind: 'event'; sequence: number; projection: unknown }
  | { kind: 'omission'; sequence: number; to: number; reason: string }

export function orderedHistoricalTrace(page: HistoryEventPage): HistoricalTraceItem[] {
  const items: HistoricalTraceItem[] = [
    ...page.events.map((event) => ({ kind: 'event' as const, sequence: event.sequence, projection: event.projection })),
    ...page.omissions.map((omission) => ({ kind: 'omission' as const, sequence: omission.from, to: omission.to, reason: omission.reason }))
  ]
  return items.sort((left, right) => left.sequence - right.sequence)
}

export type AttemptReferences = {
  check?: { id: string; kind: number; verdict: number; sequence: number }
  envelope?: { unitID: string; checkID: string; verdict: number; sequence: number }
  complete: boolean
}

const positive = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) > 0

// The stored attempt projection is a versioned positional tuple, not freeform
// event text. Unknown versions or malformed references are shown as missing.
export function attemptReferences(item: HistoryAttemptOutcome): AttemptReferences {
  const tuple = item.projection
  if (!Array.isArray(tuple) || tuple.length < 5 || tuple[0] !== 1) return { complete: false }
  const result: AttemptReferences = { complete: tuple[4] === true }
  const check = tuple[2]
  if (Array.isArray(check) && check.length === 4 && typeof check[0] === 'string' &&
      positive(check[1]) && positive(check[2]) && positive(check[3])) {
    result.check = { id: check[0], kind: check[1], verdict: check[2], sequence: check[3] }
  }
  const envelope = tuple[3]
  if (Array.isArray(envelope) && envelope.length === 4 && typeof envelope[0] === 'string' &&
      typeof envelope[1] === 'string' && positive(envelope[2]) && positive(envelope[3])) {
    result.envelope = { unitID: envelope[0], checkID: envelope[1], verdict: envelope[2], sequence: envelope[3] }
  }
  return result
}

export function historyCompleteness(detail: HistoryDetail): string[] {
  const names = ['Graph', 'Launch inputs', 'Result', 'Event trace', 'Attempt references', 'Managed evidence']
  const tuple = detail.completeness
  if (!Array.isArray(tuple) || tuple[0] !== 1) return ['Completeness unavailable']
  return names.filter((_, index) => tuple[index + 1] !== true)
}

export function historyRowIncomplete(run: HistoryRunSummary): boolean {
  if (run.storage_state !== 'complete') return true
  const tuple = run.completeness
  return !Array.isArray(tuple) || tuple[0] !== 1 || tuple.length < 7 || tuple.slice(1).some((flag) => flag !== true)
}

export function checkKindName(code: number): string {
  return ({ 1: 'Command', 2: 'JSON predicate', 3: 'Reported file scope', 4: 'Changed file scope' } as Record<number, string>)[code] ?? 'Unknown check'
}

export function verdictName(code: number): string {
  return ({ 1: 'Green', 2: 'Red' } as Record<number, string>)[code] ?? 'Unknown verdict'
}

export function projectionPreview(value: unknown, maxLength = 24_000): { text: string; truncated: boolean } {
  const raw = JSON.stringify(value, null, 2) ?? 'Unavailable'
  if (raw.length <= maxLength) return { text: raw, truncated: false }
  return { text: raw.slice(0, maxLength), truncated: true }
}

export function decodeEvidence(base64: string): Uint8Array {
  if (base64.length > 1_400_000 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(base64)) {
    throw new Error('Invalid evidence chunk')
  }
  const raw = atob(base64)
  if (raw.length > 1_048_576) throw new Error('Evidence chunk exceeds limit')
  return Uint8Array.from(raw, (char) => char.charCodeAt(0))
}

export function evidencePreview(bytes: Uint8Array): { text: string; truncated: boolean; binary: boolean } {
  try {
    const decoded = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
    if (/[\x00-\x08\x0b\x0c\x0e-\x1f]/.test(decoded)) throw new Error('binary control bytes')
    return { text: decoded.slice(0, 24_000), truncated: decoded.length > 24_000, binary: false }
  } catch {
    const limit = Math.min(bytes.length, 1024)
    const hex = Array.from(bytes.subarray(0, limit), (value) => value.toString(16).padStart(2, '0')).join(' ')
    return { text: hex, truncated: bytes.length > limit, binary: true }
  }
}
