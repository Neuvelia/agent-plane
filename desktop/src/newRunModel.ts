import { rowText, statusOf, type CapabilityResult, type CapabilityTool } from './capabilities'
import type { Definition } from './editor/types'
import type { MessageKey } from './i18n'
import type { PreflightState } from './preflight'
import type { Diagnostic } from './types'

// F2.5c: New run with preflight. Pure views of what the engine answered: the
// graph document (its declared inputs, node kinds and the Agent `isolation`
// setting as written in the file), the engine's validation and the capability
// report. Nothing here validates the graph or decides whether a run may start;
// `preflightState` (the engine's status) does that, and the engine checks again
// when the run starts.

/**
 * One declared graph input, shown as one field. `targets` are the nodes it is bound to;
 * `neededBy` are those whose bound input port is declared required, as written in the
 * file. The engine decides what that means for an optional graph input; the field only says so.
 */
export interface InputField {
  name: string
  type: string
  required: boolean
  targets: string[]
  neededBy: string[]
}

function boundPorts(def: Definition, name: string): { node: string; required: boolean }[] {
  return (def.graph_input_bindings ?? [])
    .filter((b) => b.from === `inputs.${name}`)
    .map((b) => {
      const [node = b.to, , port = ''] = b.to.split('.')
      return { node, required: def.nodes[node]?.inputs?.[port]?.required === true }
    })
}

export function inputFields(def: Definition | null): InputField[] {
  if (!def) return []
  return Object.entries(def.inputs ?? {}).map(([name, port]) => {
    const bound = boundPorts(def, name)
    return { name, type: port.type, required: port.required === true, targets: [...new Set(bound.map((b) => b.node))], neededBy: [...new Set(bound.filter((b) => b.required).map((b) => b.node))] }
  })
}

/** The `name=value` lines the run is started with; empty fields are left out. */
export function inputLines(fields: InputField[], values: Readonly<Record<string, string>>): string[] {
  return fields.flatMap((f) => {
    const v = values[f.name] ?? ''
    return v === '' ? [] : [`${f.name}=${v}`]
  })
}

/** Node count and node kinds in file order, as received. */
export function graphShape(def: Definition): { nodes: number; types: string[] } {
  const nodes = Object.values(def.nodes ?? {})
  return { nodes: nodes.length, types: [...new Set(nodes.map((n) => n.type))] }
}

/** Agent nodes and how many of them ask for their own worktree (`config.isolation: worktree`, F3.1). */
export function agentWorkspace(def: Definition | null): { agents: number; isolated: number } {
  const agents = Object.values(def?.nodes ?? {}).filter((n) => n.type === 'agent')
  return { agents: agents.length, isolated: agents.filter((n) => n.config?.isolation === 'worktree').length }
}

export type Tone = 'ok' | 'warn' | 'bad' | 'pending' | 'idle'

/** One preflight row: a glyph tone, a title and a one-line detail (both message keys with parameters, or raw text). */
export interface Check {
  id: 'graph' | 'agent' | 'inputs' | 'workspace'
  tone: Tone
  title: MessageKey
  detail?: { key: MessageKey; params?: Record<string, string | number> } | { raw: string; mono?: boolean }
}

export interface PreflightFacts {
  chosen: boolean
  validating: boolean
  validation: { status: string; diagnostics: Diagnostic[] } | null
  /** The validation request itself failed (engine unreachable, grant gone). */
  validationError: string | null
  def: Definition | null
  /** null: not asked (no Agent node); 'loading': asked, no answer yet. */
  capabilities: CapabilityResult | 'loading' | null
  values: Readonly<Record<string, string>>
}

const count = (ds: Diagnostic[], severity: string) => ds.filter((d) => d.severity === severity).length

function graphCheck(f: PreflightFacts): Check {
  if (!f.chosen) return { id: 'graph', tone: 'idle', title: 'newrun.check.graph.none', detail: { key: 'newrun.check.graph.noneDetail' } }
  if (f.validating) return { id: 'graph', tone: 'pending', title: 'newrun.check.graph.checking' }
  if (!f.validation) return { id: 'graph', tone: 'bad', title: 'newrun.check.graph.failed', detail: { raw: f.validationError ?? '' } }
  const params = { errors: count(f.validation.diagnostics, 'error'), warnings: count(f.validation.diagnostics, 'warning') }
  if (f.validation.status !== 'valid') return { id: 'graph', tone: 'bad', title: 'newrun.check.graph.invalid', detail: { key: 'newrun.check.graph.counts', params } }
  return { id: 'graph', tone: 'ok', title: 'newrun.check.graph.valid', detail: { key: 'newrun.check.graph.counts', params } }
}

function agentCheck(caps: PreflightFacts['capabilities']): Check {
  if (caps === 'loading' || caps === null) return { id: 'agent', tone: 'pending', title: 'newrun.check.codex.checking' }
  if (caps.kind !== 'ok') return { id: 'agent', tone: 'warn', title: 'newrun.check.codex.unknown', detail: { key: 'newrun.check.codex.unknownDetail' } }
  const codex: CapabilityTool | undefined = statusOf(caps.report, 'codex')
  if (codex?.status === 'available') return { id: 'agent', tone: 'ok', title: 'newrun.check.codex.found', detail: codex.version ? { raw: `codex ${codex.version}`, mono: true } : undefined }
  if (!codex) return { id: 'agent', tone: 'warn', title: 'newrun.check.codex.notReady', detail: { key: 'cap.meaning.unknown' } }
  return { id: 'agent', tone: 'warn', title: 'newrun.check.codex.notReady', detail: { key: rowText(codex).meaning } }
}

function inputsCheck(def: Definition, values: PreflightFacts['values']): Check | null {
  const required = inputFields(def).filter((i) => i.required)
  if (required.length === 0) return null
  const filled = required.filter((i) => (values[i.name] ?? '') !== '').length
  const detail = { key: 'newrun.check.inputs.count' as const, params: { filled, required: required.length } }
  return filled === required.length ? { id: 'inputs', tone: 'ok', title: 'newrun.check.inputs.filled', detail } : { id: 'inputs', tone: 'warn', title: 'newrun.check.inputs.missing', detail }
}

function workspaceCheck(def: Definition): Check | null {
  const { agents, isolated } = agentWorkspace(def)
  if (agents === 0) return null
  if (isolated === agents) return { id: 'workspace', tone: 'ok', title: 'newrun.check.ws.worktree', detail: { key: 'newrun.check.ws.worktreeDetail' } }
  return { id: 'workspace', tone: 'warn', title: 'newrun.check.ws.shared', detail: { key: 'newrun.check.ws.sharedDetail' } }
}

/** The preflight rows, in the artboard's order: graph, agent CLI, inputs, workspace. */
export function preflightChecks(f: PreflightFacts): Check[] {
  const graph = graphCheck(f)
  if (!f.chosen || !f.def) return [graph]
  const agents = agentWorkspace(f.def).agents > 0
  return [graph, agents ? agentCheck(f.capabilities) : null, inputsCheck(f.def, f.values), workspaceCheck(f.def)].filter((c): c is Check => c !== null)
}

/** Checks that did not fail count as passed; warnings are counted on their own, engine warnings included. */
export function tally(checks: Check[], diagnostics: Diagnostic[]): { passed: number; warnings: number; failed: number } {
  return {
    passed: checks.filter((c) => c.tone === 'ok' || c.tone === 'warn').length,
    warnings: checks.filter((c) => c.tone === 'warn').length + count(diagnostics, 'warning'),
    failed: checks.filter((c) => c.tone === 'bad').length,
  }
}

/** The large line under "Preflight". */
export function headline(state: PreflightState, f: Pick<PreflightFacts, 'chosen' | 'validating' | 'validation'>, warnings: number, errors: number): { key: MessageKey; params?: Record<string, number> } {
  if (!f.chosen) return { key: 'newrun.head.choose' }
  if (f.validating) return { key: 'newrun.head.checking' }
  if (!f.validation) return { key: 'newrun.head.unchecked' }
  if (state === 'blocked') return errors > 0 ? { key: 'newrun.head.blockedBy', params: { count: errors } } : { key: 'newrun.head.blocked' }
  return warnings > 0 ? { key: 'newrun.head.warnings', params: { count: warnings } } : { key: 'newrun.head.ready' }
}
