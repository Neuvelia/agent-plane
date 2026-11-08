import { describe, expect, it } from 'vitest'
import type { EvidenceDescriptor, HistoryAttemptOutcome, HistoryDetail, HistoryEventPage, HistoryRunSummary } from '../preload/api'
import { adaptiveHistoryPage, attemptReferences, checkKindName, decodeEvidence, evidencePreview, evidenceScopeLabel, historyCompleteness, historyRowIncomplete, orderedHistoricalTrace, projectionPreview, verdictName } from './historyData'

describe('historical inspector data', () => {
  it('reads typed Check Result and Return Envelope references without interpreting arbitrary prose', () => {
    const item: HistoryAttemptOutcome = {
      attempt_id: 'attempt-1', node_run_id: 'node-1', status: 'accepted', digest: 'digest',
      projection: [1, 'node', ['check-1', 2, 1, 9], ['unit-1', 'check-1', 1, 10], true]
    }
    expect(attemptReferences(item)).toEqual({
      check: { id: 'check-1', kind: 2, verdict: 1, sequence: 9 },
      envelope: { unitID: 'unit-1', checkID: 'check-1', verdict: 1, sequence: 10 }, complete: true
    })
    expect(attemptReferences({ ...item, projection: [2, 'secret', 'text'] })).toEqual({ complete: false })
    expect(checkKindName(2)).toBe('JSON predicate')
    expect(verdictName(1)).toBe('Green')
  })

  it('marks every omitted final projection independently', () => {
    const detail = { completeness: [1, true, false, true, false, false, true] } as HistoryDetail
    expect(historyCompleteness(detail)).toEqual(['Launch inputs', 'Event trace', 'Attempt references'])
    expect(historyCompleteness({ completeness: null } as HistoryDetail)).toEqual(['Completeness unavailable'])
    const row = { storage_state: 'complete', completeness: [1, true, true, false, true, true, true] } as HistoryRunSummary
    expect(historyRowIncomplete(row)).toBe(true)
    expect(historyRowIncomplete({ ...row, completeness: [1, true, true, true, true, true, true] })).toBe(false)
  })

  it('bounds large previews and rejects malformed evidence chunks', () => {
    expect(projectionPreview({ value: 'hello' }, 8)).toEqual({ text: '{\n  "val', truncated: true })
    expect(Array.from(decodeEvidence('c2FmZQ=='))).toEqual([115, 97, 102, 101])
    expect(() => decodeEvidence('a$')).toThrow('Invalid evidence chunk')
    expect(evidencePreview(new Uint8Array([0xff, 0x00, 0x41]))).toEqual({ text: 'ff 00 41', truncated: false, binary: true })
    expect(evidencePreview(new TextEncoder().encode('safe'))).toEqual({ text: 'safe', truncated: false, binary: false })
  })

  it('labels one-shot evidence by node without inventing an attempt', () => {
    const node = { node_run_id: 'node-1' } as EvidenceDescriptor
    const attempt = { attempt_id: 'attempt-1', node_run_id: 'node-1' } as EvidenceDescriptor
    expect(evidenceScopeLabel(node)).toBe('Node evidence · node-1')
    expect(evidenceScopeLabel(attempt)).toBe('Attempt · attempt-1')
  })

  it('keeps retained facts and omitted ranges in original sequence order', () => {
    const page = { events: [{ sequence: 1, projection: {}, digest: 'a' }, { sequence: 3, projection: {}, digest: 'b' }],
      omissions: [{ from: 2, to: 2, reason: 'not_retained' }], next_sequence: 3, watermark: 3, finished: true,
      complete: false, completeness_digest: 'c' } as HistoryEventPage
    expect(orderedHistoricalTrace(page).map((item) => item.sequence)).toEqual([1, 2, 3])
  })

  it('retries a historical page with smaller limits and stops at one', async () => {
    const calls: number[] = []
    const value = await adaptiveHistoryPage(async (limit) => {
      calls.push(limit)
      if (limit > 2) throw Object.assign(new Error('too large'), { code: 'page_too_large' })
      return { limit }
    }, 9)
    expect(calls).toEqual([9, 4, 2])
    expect(value).toEqual({ limit: 2 })
    await expect(adaptiveHistoryPage(async () => { throw Object.assign(new Error('too large'), { code: 'page_too_large' }) }, 1)).rejects.toThrow('too large')
  })
})
