import type { MessageKey } from './i18n'
import { en } from './i18n/en'

// F2.11 capability report (ADR-0025), read only through the two allowlisted routes.

export interface CapabilityTool {
  id: string
  kind: string
  status: string
  version: string
  reason: string
}

export interface CapabilityReport {
  version: number
  checked_at: string
  engine: { version: string; os: string; arch: string }
  tools: CapabilityTool[]
}

/** How a read ended: a report, an answer that is not one, or no engine to ask. */
export type CapabilityResult = { kind: 'ok'; report: CapabilityReport } | { kind: 'failed'; status: number } | { kind: 'unreachable' }

function isReport(b: unknown): b is CapabilityReport {
  const r = b as CapabilityReport | null
  return !!r && typeof r === 'object' && typeof r.version === 'number' && typeof r.checked_at === 'string' && !!r.engine && typeof r.engine.version === 'string' && Array.isArray(r.tools) && r.tools.every((t) => t && typeof t.id === 'string' && typeof t.status === 'string')
}

async function read(method: 'GET' | 'POST', path: string, body?: unknown): Promise<CapabilityResult> {
  let r
  try {
    r = await window.agentplane.request<CapabilityReport>(method, path, body)
  } catch {
    return { kind: 'unreachable' }
  }
  return r.status === 200 && isReport(r.body) ? { kind: 'ok', report: r.body } : { kind: 'failed', status: r.status }
}

export const getCapabilities = (): Promise<CapabilityResult> => read('GET', '/capabilities')
export const checkCapabilities = (): Promise<CapabilityResult> => read('POST', '/capabilities/check', {})

// A status, reason or id this version does not know is shown verbatim with a
// neutral glyph and is never treated as available.

const has = (k: string): k is MessageKey => Object.prototype.hasOwnProperty.call(en, k)

export type Glyph = '✓' | '◐' | '○' | '✕' | '⊘' | '!' | '?'
const GLYPHS: Record<string, Glyph> = {
  available: '✓',
  detected_no_adapter: '◐',
  not_configured: '○',
  missing: '✕',
  unsupported_platform: '⊘',
  unsupported_version: '⊘',
  error: '!',
}

export interface RowText {
  /** Localized product name, or the id as received. */
  name: { key: MessageKey } | { raw: string }
  /** Localized status word, or the status as received. */
  status: { key: MessageKey } | { raw: string }
  glyph: Glyph
  known: boolean
  /** One sentence on what the status means. */
  meaning: MessageKey
  /** What still works; absent for `available`. */
  still?: MessageKey
}

function meaningOf(t: CapabilityTool): { meaning: MessageKey; still?: MessageKey } {
  const s = t.status
  if (s === 'error') return { meaning: 'cap.meaning.error', still: t.id === 'git' ? 'cap.still.shell' : t.id === 'shell' ? 'cap.still.noShell' : t.id === 'docker' ? undefined : t.kind === 'agent_cli' ? 'cap.still.cli' : 'cap.still.noDesktopAgents' }
  if (t.id === 'git' && t.kind === 'tool') {
    if (s === 'available') return { meaning: 'cap.meaning.git.available' }
    if (s === 'missing' && t.reason === 'developer_tools_missing') return { meaning: 'cap.meaning.git.devtools', still: 'cap.still.shell' }
    if (s === 'missing') return { meaning: 'cap.meaning.git.missing', still: 'cap.still.shell' }
    if (s === 'unsupported_platform') return { meaning: 'cap.meaning.git.platform', still: 'cap.still.shell' }
  }
  if (t.id === 'codex' && t.kind === 'agent_adapter') {
    if (s === 'available') return { meaning: 'cap.meaning.codex.available' }
    if (s === 'not_configured' && t.reason === 'found_on_path') return { meaning: 'cap.meaning.codex.foundOnPath', still: 'cap.still.noDesktopAgents' }
    if (s === 'not_configured') return { meaning: 'cap.meaning.codex.notConfigured', still: 'cap.still.noDesktopAgents' }
    if (s === 'missing') return { meaning: 'cap.meaning.codex.missing', still: 'cap.still.noDesktopAgents' }
    if (s === 'unsupported_version') return { meaning: 'cap.meaning.codex.version', still: 'cap.still.noDesktopAgents' }
    if (s === 'unsupported_platform') return { meaning: 'cap.meaning.codex.platform', still: 'cap.still.noDesktopAgents' }
  }
  if (t.id === 'shell' && t.kind === 'tool') {
    if (s === 'available') return { meaning: 'cap.meaning.shell.available' }
    if (s === 'missing') return { meaning: 'cap.meaning.shell.missing', still: 'cap.still.noShell' }
    if (s === 'unsupported_platform') return { meaning: 'cap.meaning.shell.platform', still: 'cap.still.noShell' }
  }
  if (t.id === 'docker' && t.kind === 'tool') {
    if (s === 'available') return { meaning: 'cap.meaning.docker.available' }
    if (s === 'missing') return { meaning: 'cap.meaning.docker.missing' }
  }
  if (t.kind === 'agent_cli') {
    if (s === 'detected_no_adapter') return { meaning: 'cap.meaning.cli.found', still: 'cap.still.cli' }
    if (s === 'missing') return { meaning: 'cap.meaning.cli.missing', still: 'cap.still.cli' }
  }
  return { meaning: 'cap.meaning.unknown' }
}

/** The localized product name of a tool id, or the id as received. */
export function toolName(id: string): RowText['name'] {
  const key = `cap.tool.${id}`
  return has(key) ? { key } : { raw: id }
}

export function rowText(t: CapabilityTool): RowText {
  const statusKey = `cap.status.${t.status}`
  const known = has(statusKey)
  return {
    name: toolName(t.id),
    status: known ? { key: statusKey } : { raw: t.status },
    glyph: GLYPHS[t.status] ?? '?',
    known,
    ...meaningOf(t),
  }
}

/** The explanation for a reason code, when this version knows it; the code itself is always shown verbatim. */
export function reasonKey(reason: string): MessageKey | undefined {
  const k = `cap.reason.${reason}`
  return has(k) ? k : undefined
}

/** The report row for a tool id; `undefined` means the report does not list it (or there is no report). */
export function statusOf(report: CapabilityReport | null, id: string): CapabilityTool | undefined {
  return report?.tools.find((t) => t.id === id)
}

export const WELCOME_KEY ='agentplane.welcome.v1'

function safeStorage(): Storage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage
  } catch {
    return undefined
  }
}

/** True when the welcome screen was completed. Missing or throwing storage means "not completed". */
export function welcomeDone(storage: Pick<Storage, 'getItem'> | undefined = safeStorage()): boolean {
  try {
    return storage?.getItem(WELCOME_KEY) === 'done'
  } catch {
    return false
  }
}

/** Records completion. Unavailable storage is silent: the screen then opens again next launch. */
export function markWelcomeDone(storage: Pick<Storage, 'setItem'> | undefined = safeStorage()): void {
  try {
    storage?.setItem(WELCOME_KEY, 'done')
  } catch {
    // Session-only completion.
  }
}
