import agentScopedApproval from '../../.agents/templates/agent-scoped-approval.yaml?raw'
import humanApproval from '../../.agents/templates/human-approval.yaml?raw'
import index from '../../.agents/templates/index.json'
import parallelShell from '../../.agents/templates/parallel-shell.yaml?raw'
import shellCheckLoop from '../../.agents/templates/shell-check-loop.yaml?raw'
import { statusOf, type CapabilityReport, type CapabilityTool } from './capabilities'
import { graphIdFromLabel } from './editor/doc'

// F2.12: the bundled pattern templates. They are ordinary graph files from
// .agents/templates, imported as text at build time; nothing is fetched or read
// at runtime. The renderer copies a template and replaces its single `id:` line;
// it never parses or validates the YAML (the engine does, as for any draft).

export interface TemplateEntry {
  id: string
  text: string
  requires: string[]
  platforms: string[]
  /** Number of values the person must fill (the index's placeholders). */
  placeholders: number
  /** Node types shown in the picker. test/templates.test.tsx checks them against the files. */
  nodeTypes: string[]
}

const TEXT: Record<string, string> = {
  'parallel-shell': parallelShell,
  'shell-check-loop': shellCheckLoop,
  'human-approval': humanApproval,
  'agent-scoped-approval': agentScopedApproval,
}
const NODE_TYPES: Record<string, string[]> = {
  'parallel-shell': ['shell'],
  'shell-check-loop': ['shell'],
  'human-approval': ['shell', 'human'],
  'agent-scoped-approval': ['human', 'agent', 'shell'],
}

/** Picker order (F2.12 requirement 7); `parallel-shell` is the suggested first graph. */
export const TEMPLATE_ORDER = ['parallel-shell', 'shell-check-loop', 'human-approval', 'agent-scoped-approval'] as const
export type TemplateId = (typeof TEMPLATE_ORDER)[number]
export const SUGGESTED: TemplateId = 'parallel-shell'

type IndexEntry = { id: string; requires: string[]; platforms: string[]; placeholders: unknown[] }
export const TEMPLATES: TemplateEntry[] = TEMPLATE_ORDER.flatMap((id) => {
  const e = (index.templates as IndexEntry[]).find((t) => t.id === id)
  const text = TEXT[id]
  return e && text !== undefined ? [{ id, text, requires: [...e.requires], platforms: [...e.platforms], placeholders: e.placeholders.length, nodeTypes: NODE_TYPES[id] ?? [] }] : []
})

/**
 * The draft for a template: its text with exactly the one `id: <template id>`
 * line replaced. Null when the text does not have exactly one such line, so the
 * entry is shown as unavailable instead of producing a wrong graph.
 */
export function templateDraft(t: Pick<TemplateEntry, 'id' | 'text'>, label: string): string | null {
  const line = `id: ${t.id}`
  const lines = t.text.split('\n')
  const at = lines.flatMap((l, i) => (l === line || l === `${line}\r` ? [i] : []))
  if (at.length !== 1) return null
  const i = at[0]!
  lines[i] = `id: ${graphIdFromLabel(label)}${lines[i]!.endsWith('\r') ? '\r' : ''}`
  return lines.join('\n')
}

export type Notice = { kind: 'platform' } | { kind: 'requires'; id: string; tool: CapabilityTool | undefined; checked: boolean } | { kind: 'cliOnly' }

/** Notices for a template from the capability report (null when none is available). They inform; nothing is disabled. */
export function noticesFor(t: TemplateEntry, report: CapabilityReport | null): Notice[] {
  const out: Notice[] = []
  if (report && !t.platforms.includes(report.engine.os)) out.push({ kind: 'platform' })
  for (const id of t.requires) {
    out.push({ kind: 'requires', id, tool: statusOf(report, id), checked: report !== null })
    if (id === 'codex') out.push({ kind: 'cliOnly' })
  }
  return out
}
