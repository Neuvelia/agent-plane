import type { RecentGraph } from '../types'
import type { OpenResponse, SaveOutcome, StatResponse, ValidateResponse } from './types'

// Thin wrappers over the preload bridge for the graph document API (ADR-0021).
const bridge = () => window.agentplane

interface ErrorBody {
  error?: { code?: string; message?: string }
  current_hash?: string
}

const failure = (r: { status: number; body: ErrorBody | null }) => ({
  ok: false as const,
  code: r.body?.error?.code ?? 'unavailable',
  message: r.body?.error?.message ?? `request failed (${r.status})`,
})

export async function openDocument(grant: string): Promise<{ ok: true; doc: OpenResponse } | { ok: false; code: string; message: string }> {
  const r = await bridge().request<OpenResponse & ErrorBody>('POST', '/graphs/open', { grant })
  if (r.status === 200 && r.body) return { ok: true, doc: r.body }
  return failure(r)
}

/** Validates unsaved text. Null means the engine could not be reached. */
export async function validateText(text: string): Promise<ValidateResponse | null> {
  try {
    const r = await bridge().request<ValidateResponse>('POST', '/graphs/validate', { text })
    return r.status === 200 && r.body ? r.body : null
  } catch {
    return null
  }
}

export async function saveDocument(grant: string, baseHash: string | null, text: string): Promise<SaveOutcome> {
  try {
    const r = await bridge().request<{ hash?: string } & ErrorBody>('PUT', '/graphs/save', { grant, base_hash: baseHash, text })
    if (r.status === 200 && r.body?.hash) return { kind: 'saved', hash: r.body.hash }
    const code = r.body?.error?.code
    if (r.status === 409 && code === 'conflict' && r.body?.current_hash) return { kind: 'conflict', currentHash: r.body.current_hash }
    if (r.status === 409 && code === 'exists') return { kind: 'exists' }
    if (r.status === 409 && code === 'gone') return { kind: 'gone' }
    const f = failure(r)
    return { kind: 'failed', code: f.code, message: f.message }
  } catch (e) {
    return { kind: 'failed', code: 'unavailable', message: e instanceof Error ? e.message : 'the engine could not be reached' }
  }
}

export async function statDocument(grant: string): Promise<StatResponse | null> {
  try {
    const r = await bridge().request<StatResponse>('POST', '/graphs/stat', { grant })
    return r.status === 200 && r.body ? r.body : null
  } catch {
    return null
  }
}

export const recentGraphs = (): Promise<RecentGraph[]> => bridge().recentGraphs().catch(() => [])
