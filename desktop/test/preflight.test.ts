import { describe, expect, it } from 'vitest'
import { canStart, preflightState } from '../src/preflight'
import type { Diagnostic } from '../src/types'

const d = (severity: string): Diagnostic => ({ severity, code: 'X', message: 'm' })

describe('preflightState', () => {
  it('is idle until the engine has answered', () => {
    expect(preflightState(null)).toBe('idle')
  })
  it('is ready for a valid graph without warnings', () => {
    expect(preflightState({ status: 'valid', diagnostics: [] })).toBe('ready')
  })
  it('is warnings for a valid graph with warnings, and still allows a run', () => {
    const s = preflightState({ status: 'valid', diagnostics: [d('warning')] })
    expect(s).toBe('warnings')
    expect(canStart(s)).toBe(true)
  })
  it('is blocked whenever the engine says the graph is not valid', () => {
    expect(preflightState({ status: 'invalid', diagnostics: [d('error')] })).toBe('blocked')
    expect(preflightState({ status: 'invalid', diagnostics: [] })).toBe('blocked')
    expect(preflightState({ status: 'whatever', diagnostics: [] })).toBe('blocked')
  })
  it('never allows a run before validation or when blocked', () => {
    expect(canStart('idle')).toBe(false)
    expect(canStart('blocked')).toBe(false)
    expect(canStart('ready')).toBe(true)
  })
})
