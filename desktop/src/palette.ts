// F2.5b command palette: the pure model. Which entries a query shows, in
// which order, which one is active, and which global key press means what.
// No DOM, no React, no engine calls: the view (ui/CommandPalette.tsx) and the
// shell (App.tsx) feed it plain data and act on what it returns.

/** Groups in display order. A run entry belongs to "runs"; screens to "goto". */
export type PaletteGroup = 'actions' | 'runs' | 'goto'
export const GROUP_ORDER: readonly PaletteGroup[] = ['actions', 'runs', 'goto']

/** A shortcut shown next to an entry and recognised by matchShortcut. */
export type Shortcut = { kind: 'chord'; key: string; shift?: boolean } | { kind: 'sequence'; keys: readonly [string, string] }

export interface PaletteEntry {
  id: string
  group: PaletteGroup
  label: string
  /** Muted text before the shortcut: a count, or a run's short id and status. */
  hint: string
  /** Extra text that matches a query but is not shown (a run's full id). */
  terms?: string
  /** Matched by the "#" prefix against the start of this value. */
  runId?: string
  shortcut?: Shortcut
}

/** "#" narrows to runs (matched by id), ">" to actions. */
export type Scope = 'all' | 'actions' | 'runs'
const PREFIX: Record<Exclude<Scope, 'all'>, string> = { actions: '>', runs: '#' }
const SCOPES: readonly Scope[] = ['all', 'actions', 'runs']

/** How many runs the palette lists without a query, and at most with one (bounded rows). */
export const RECENT_RUNS = 5
export const MAX_RUNS = 20

export function parseQuery(raw: string): { scope: Scope; text: string } {
  const s = raw.trimStart()
  if (s.startsWith(PREFIX.actions)) return { scope: 'actions', text: s.slice(1).trim().toLowerCase() }
  if (s.startsWith(PREFIX.runs)) return { scope: 'runs', text: s.slice(1).trim().toLowerCase() }
  return { scope: 'all', text: s.trim().toLowerCase() }
}

/** Tab moves to the next scope (Shift+Tab to the previous) by rewriting the query's prefix. */
export function cycleScope(raw: string, step: 1 | -1): string {
  const { scope } = parseQuery(raw)
  const rest = scope === 'all' ? raw.trimStart() : raw.trimStart().slice(1).trimStart()
  const next = SCOPES[(SCOPES.indexOf(scope) + step + SCOPES.length) % SCOPES.length]!
  return next === 'all' ? rest : `${PREFIX[next]}${rest}`
}

function matches(e: PaletteEntry, scope: Scope, text: string, groupLabel: string): boolean {
  if (scope === 'actions' && e.group !== 'actions') return false
  if (scope === 'runs') return e.group === 'runs' && (!text || (e.runId ?? '').toLowerCase().startsWith(text) || e.label.toLowerCase().includes(text))
  if (!text) return true
  return `${e.label} ${e.hint} ${groupLabel} ${e.terms ?? ''}`.toLowerCase().includes(text)
}

/** The entries a query shows, grouped in GROUP_ORDER, each group in its given order. Runs are bounded. */
export function filterEntries<T extends PaletteEntry>(entries: readonly T[], raw: string, groupLabel: (g: PaletteGroup) => string): T[] {
  const { scope, text } = parseQuery(raw)
  const runLimit = text || scope === 'runs' ? MAX_RUNS : RECENT_RUNS
  const out: T[] = []
  for (const g of GROUP_ORDER) {
    const hits = entries.filter((e) => e.group === g && matches(e, scope, text, groupLabel(g)))
    out.push(...(g === 'runs' ? hits.slice(0, runLimit) : hits))
  }
  return out
}

/** Arrow keys move the active row and wrap around. An empty list has no active row (-1). */
export function moveActive(active: number, count: number, key: 'ArrowDown' | 'ArrowUp'): number {
  if (count === 0) return -1
  const from = active < 0 ? (key === 'ArrowDown' ? -1 : 0) : active
  return (from + (key === 'ArrowDown' ? 1 : -1) + count) % count
}

/** Global commands the shell answers outside the palette. */
export type GlobalCommand = 'palette' | 'newRun' | 'openGraph' | 'approvals' | 'settings' | 'dashboard'

/** One key press, as much of a KeyboardEvent as the matcher needs. */
export interface KeyPress {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  shiftKey: boolean
  altKey: boolean
  /** The focus is in a text field: plain letter sequences do not apply there. */
  typing: boolean
}

export const SHORTCUTS: Record<GlobalCommand, Shortcut> = {
  palette: { kind: 'chord', key: 'K' },
  newRun: { kind: 'chord', key: 'N' },
  openGraph: { kind: 'chord', key: 'O' },
  approvals: { kind: 'chord', key: 'A', shift: true },
  settings: { kind: 'chord', key: ',' },
  dashboard: { kind: 'sequence', keys: ['G', 'D'] },
}

/** Tracks the first key of a sequence between presses. */
export interface ShortcutState {
  pending: string | null
}

/**
 * Matches a key press against SHORTCUTS. Chords use Cmd on macOS and Ctrl elsewhere.
 * A sequence ("G" then "D") only counts while no text field has focus, so typing never navigates.
 */
export function matchShortcut(state: ShortcutState, k: KeyPress, mac: boolean): { state: ShortcutState; command: GlobalCommand | null } {
  const mod = mac ? k.metaKey && !k.ctrlKey : k.ctrlKey && !k.metaKey
  const key = k.key.length === 1 ? k.key.toUpperCase() : k.key
  if (mod && !k.altKey) return { state: { pending: null }, command: findCommand((s) => s.kind === 'chord' && s.key === key && Boolean(s.shift) === k.shiftKey) }
  if (k.typing || k.ctrlKey || k.metaKey || k.altKey || key.length !== 1) return { state: { pending: null }, command: null }
  const done = findCommand((s) => s.kind === 'sequence' && state.pending === s.keys[0] && key === s.keys[1])
  if (done) return { state: { pending: null }, command: done }
  const starts = Object.values(SHORTCUTS).some((s) => s.kind === 'sequence' && s.keys[0] === key)
  return { state: { pending: starts ? key : null }, command: null }
}

function findCommand(test: (s: Shortcut) => boolean): GlobalCommand | null {
  const hit = (Object.entries(SHORTCUTS) as [GlobalCommand, Shortcut][]).find(([, s]) => test(s))
  return hit ? hit[0] : null
}

/** Shortcut as written on screen: "Ctrl Shift A" on Windows and Linux, "⌘ ⇧ A" on macOS, "G D" for a sequence. */
export function shortcutText(s: Shortcut, mac: boolean): string {
  if (s.kind === 'sequence') return s.keys.join(' ')
  return [mac ? '⌘' : 'Ctrl', ...(s.shift ? [mac ? '⇧' : 'Shift'] : []), s.key].join(' ')
}

/** The fields of a run summary the palette lists. */
export interface PaletteRun {
  run_id: string
  graph_id: string
  started_at: string
  status: string
}

/** Runs newest first, labelled by graph, hinted by short id and status (as the runs list shows them). */
export function runEntries(runs: readonly PaletteRun[], statusText: (status: string) => string): PaletteEntry[] {
  return [...runs]
    .sort((a, b) => (a.started_at < b.started_at ? 1 : a.started_at > b.started_at ? -1 : 0))
    .map((r) => ({ id: `run:${r.run_id}`, group: 'runs', label: r.graph_id, hint: `${r.run_id.slice(0, 8)} · ${statusText(r.status)}`, terms: r.run_id, runId: r.run_id }))
}
