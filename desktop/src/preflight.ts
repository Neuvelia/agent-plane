import type { Diagnostic } from './types'

export type PreflightState = 'idle' | 'ready' | 'warnings' | 'blocked'

/** What the preflight card says about a graph, from the engine's validation result.
 *  Only `status === 'valid'` allows a run; warnings never block, errors do. */
export function preflightState(result: { status: string; diagnostics: Diagnostic[] } | null): PreflightState {
  if (!result) return 'idle'
  if (result.status !== 'valid') return 'blocked'
  return result.diagnostics.some((d) => d.severity === 'warning') ? 'warnings' : 'ready'
}

export const canStart = (state: PreflightState): boolean => state === 'ready' || state === 'warnings'
