import { statusOf, type CapabilityReport, type CapabilityTool } from './capabilities'
import type { MessageKey } from './i18n'
import { SUGGESTED, TEMPLATES, templateDraft, type TemplateEntry } from './templates'

// Pure model of the four-step setup (canvas "Setup, revised"). The views only
// render what these functions decide; nothing here reads the bridge.

export const SETUP_STEPS = ['welcome', 'check', 'agents', 'graph'] as const
export type SetupStep = (typeof SETUP_STEPS)[number]

/** How a capability row is drawn: a glyph shape and a status colour, always next to the status word. */
export type RowTone = 'ok' | 'warn' | 'off' | 'bad'

const TONES: Record<string, RowTone> = {
  available: 'ok',
  not_configured: 'warn',
  missing: 'warn',
  unsupported_version: 'warn',
  detected_no_adapter: 'off',
  unsupported_platform: 'off',
  error: 'bad',
}

/**
 * The tone of a report row; an unknown status is neutral, never "ok". An agent
 * CLI has no adapter in any case and Docker is optional, so their absence is
 * neutral rather than a warning.
 */
export function rowTone(tool: Pick<CapabilityTool, 'id' | 'kind' | 'status'>): RowTone {
  const tone = TONES[tool.status] ?? 'off'
  if (tone !== 'warn') return tone
  return tool.kind === 'agent_cli' || tool.id === 'docker' ? 'off' : tone
}

export type CheckState = { state: 'loading' } | { state: 'loaded'; report: CapabilityReport; refreshing: boolean } | { state: 'failed'; status: number } | { state: 'disconnected' }

export interface Verdict {
  tone: 'ok' | 'warn' | 'bad' | 'busy'
  key: MessageKey
}

/**
 * The one-line answer above the rows. The desktop runs shell, gate and human
 * nodes; agent nodes run from the CLI in this version (F2.11 requirement 11), so
 * the verdict depends on the engine answering and on the shell row only.
 */
export function verdictOf(view: CheckState): Verdict {
  if (view.state === 'loading' || (view.state === 'loaded' && view.refreshing)) return { tone: 'busy', key: 'setup.check.checking' }
  if (view.state === 'disconnected') return { tone: 'bad', key: 'setup.check.lost' }
  if (view.state === 'failed') return { tone: 'bad', key: 'welcome.failed' }
  return statusOf(view.report, 'shell')?.status === 'available' ? { tone: 'ok', key: 'setup.check.ready' } : { tone: 'warn', key: 'setup.check.noShell' }
}

/** Seconds after the rows mount at which row `i` lands; the verdict lands after the last one. */
export const scanDelay = (i: number) => ({ row: 0.2 + i * 0.12, settle: 0.8 + i * 0.18 })
export const verdictDelay = (rows: number) => scanDelay(Math.max(0, rows - 1)).settle + 0.3

/** Onboarding cards: the suggested graph, one agent graph, one human graph and Blank. */
export const SETUP_TEMPLATES = [SUGGESTED, 'dependency-update', 'human-approval'] as const
export const BLANK = 'blank'

export interface SetupCard {
  id: string
  /** Node types in file order; empty for Blank. */
  flow: string[]
  recommended: boolean
  placeholders: number
  agent: boolean
}

export function setupCards(templates: readonly TemplateEntry[] = TEMPLATES): SetupCard[] {
  const cards = SETUP_TEMPLATES.flatMap((id) => {
    const e = templates.find((t) => t.id === id)
    return e ? [{ id, flow: e.flow, recommended: id === SUGGESTED, placeholders: e.placeholders, agent: e.flow.includes('agent') }] : []
  })
  return [...cards, { id: BLANK, flow: [], recommended: false, placeholders: 0, agent: false }]
}

/** What the last step creates: Blank (no draft maker) or the picked template's draft. */
export type SetupDraft = { usable: true; make: ((label: string) => string | null) | undefined } | { usable: false }

/**
 * The graph the primary action of the last step creates. A template that is
 * missing from the build, or whose text cannot take a new id, is unusable, so
 * the action is disabled instead of creating the wrong graph.
 */
export function setupDraft(choice: string, templates: readonly TemplateEntry[] = TEMPLATES): SetupDraft {
  if (choice === BLANK) return { usable: true, make: undefined }
  const entry = templates.find((t) => t.id === choice)
  if (!entry || templateDraft(entry, 'probe.yaml') === null) return { usable: false }
  return { usable: true, make: (label) => templateDraft(entry, label) }
}

/** The command shown on the Agents step: run a graph with the Codex adapter from the CLI. */
export const CLI_COMMAND = 'agentplane run <graph.yaml> --adapter codex --codex-bin <path>'
