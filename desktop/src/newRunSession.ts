import type { ApiFailure } from './api'
import type { CapabilityResult } from './capabilities'
import type { Definition } from './editor/types'
import { agentWorkspace, inputFields, inputLines } from './newRunModel'
import type { ChosenGraph, Diagnostic, GraphGrantResult, RecentGraph } from './types'

// F2.5c: the state behind New run, without React so its ordering rules are
// testable. The graph is addressed by grant only; its path stays in the main
// process (ADR-0021). Rules:
// - only the newest pick counts; a grant from an older pick, or one that arrives
//   after the screen was left, is released at once;
// - answers for a graph that is no longer chosen are dropped;
// - the previous graph's grant is released when another is adopted, and the
//   chosen one when the screen is left;
// - the graph cannot change while a run is being started.

/** What the session needs from the engine and the main process. */
export interface NewRunPorts {
  openGraph(): Promise<GraphGrantResult>
  openRecentGraph(id: string): Promise<GraphGrantResult>
  recentGraphs(): Promise<RecentGraph[]>
  closeGraph(grant: string): Promise<unknown>
  validate(grant: string): Promise<{ status: string; diagnostics: Diagnostic[] } | ApiFailure>
  /** The engine's definition of the granted file, or null when it cannot be read. */
  definition(grant: string): Promise<Definition | null>
  start(grant: string, inputs: string[]): Promise<{ runId: string } | ApiFailure>
  capabilities(): Promise<CapabilityResult>
}

/** Which option of the graph list is chosen: a recent graph by id, or a file chosen with the dialog. */
export type GraphSource = { kind: 'recent'; id: string } | { kind: 'file' }

export interface NewRunState {
  graph: ChosenGraph | null
  source: GraphSource | null
  recent: RecentGraph[]
  def: Definition | null
  values: Readonly<Record<string, string>>
  validation: { status: string; diagnostics: Diagnostic[] } | null
  validationError: string | null
  validating: boolean
  /** Choosing a graph failed (dialog or recent entry); not a refused start. */
  pickError: string | null
  startError: ApiFailure | null
  busy: boolean
  capabilities: CapabilityResult | 'loading' | null
}

const message = (e: unknown, fallback: string) => (e instanceof Error ? e.message : fallback)
const unreachable = (e: unknown): ApiFailure => ({ message: message(e, 'the engine could not be reached'), diagnostics: [] })

export class NewRunSession {
  private state: NewRunState
  private readonly listeners = new Set<() => void>()
  private pick = 0
  private disposed = false
  private started = false

  constructor(
    private readonly ports: NewRunPorts,
    private readonly initial?: ChosenGraph,
  ) {
    this.state = { graph: initial ?? null, source: initial ? { kind: 'file' } : null, recent: [], def: null, values: {}, validation: null, validationError: null, validating: false, pickError: null, startError: null, busy: false, capabilities: null }
  }

  readonly snapshot = (): NewRunState => this.state

  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  /** Reads the recent list and checks a graph passed in (Inbox "Open graph"). Runs once. */
  start(): void {
    if (this.started) return
    this.started = true
    void this.refreshRecent()
    if (this.initial) void this.inspect(this.initial, false)
  }

  /** Leaving the screen: late answers are dropped and the chosen grant is released. */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    if (this.state.graph) this.release(this.state.graph.grant)
  }

  setValue(name: string, value: string): void {
    this.set({ values: { ...this.state.values, [name]: value } })
  }

  chooseFile(): Promise<void> {
    return this.choose(() => this.ports.openGraph(), () => ({ kind: 'file' }))
  }

  chooseRecent(r: RecentGraph): Promise<void> {
    return this.choose(() => this.ports.openRecentGraph(r.id), () => ({ kind: 'recent', id: r.id }))
  }

  /** Checks the chosen graph again (the file may have been saved meanwhile), keeping what is on screen until the answer arrives. */
  recheck(): void {
    const g = this.state.graph
    if (g && !this.state.validating && !this.state.busy && !this.disposed) void this.inspect(g, true)
  }

  async run(onStarted: (runId: string) => void): Promise<void> {
    const g = this.state.graph
    if (!g || this.state.busy) return
    this.set({ busy: true, startError: null })
    const r = await this.ports.start(g.grant, inputLines(inputFields(this.state.def), this.state.values)).catch(unreachable)
    if (this.disposed) return
    this.set({ busy: false })
    if ('message' in r) this.set({ startError: r })
    else onStarted(r.runId)
  }

  private set(patch: Partial<NewRunState>): void {
    this.state = { ...this.state, ...patch }
    for (const l of this.listeners) l()
  }

  private release(grant: string): void {
    void this.ports.closeGraph(grant).catch(() => undefined)
  }

  private isCurrent(g: ChosenGraph): boolean {
    return !this.disposed && this.state.graph?.grant === g.grant
  }

  private async refreshRecent(): Promise<void> {
    const recent = await this.ports.recentGraphs().catch(() => [])
    if (!this.disposed) this.set({ recent })
  }

  private async choose(ask: () => Promise<GraphGrantResult>, source: () => GraphSource): Promise<void> {
    if (this.state.busy || this.disposed) return
    const pick = ++this.pick
    let picked: GraphGrantResult
    try {
      picked = await ask()
    } catch (e) {
      if (pick === this.pick && !this.disposed) this.set({ pickError: message(e, 'the graph could not be opened') })
      return
    }
    if ('canceled' in picked) return
    if (pick !== this.pick || this.disposed) {
      if (picked.ok) this.release(picked.grant) // an older pick, or the screen is gone
      return
    }
    if (!picked.ok) {
      this.set({ pickError: picked.message })
      void this.refreshRecent() // the main process drops a recent entry it cannot open
      return
    }
    this.adopt({ grant: picked.grant, label: picked.label }, source())
  }

  private adopt(chosen: ChosenGraph, source: GraphSource): void {
    const previous = this.state.graph
    this.set({ graph: chosen, source, values: {}, pickError: null })
    if (previous && previous.grant !== chosen.grant) this.release(previous.grant)
    void this.inspect(chosen, false)
  }

  private async inspect(g: ChosenGraph, quiet: boolean): Promise<void> {
    if (!quiet) this.set({ validation: null, validationError: null, startError: null, def: null, validating: true })
    const [checked, def] = await Promise.all([this.ports.validate(g.grant).catch(unreachable), this.ports.definition(g.grant).catch(() => null)])
    if (!this.isCurrent(g)) return // another graph was chosen meanwhile, or the screen was left
    this.set({ validating: false, validation: 'message' in checked ? null : checked, validationError: 'message' in checked ? checked.message : null, def })
    if (agentWorkspace(def).agents > 0 && this.state.capabilities === null) void this.readCapabilities()
  }

  // Read once, and only for a graph with an Agent node.
  private async readCapabilities(): Promise<void> {
    this.set({ capabilities: 'loading' })
    const r = await this.ports.capabilities().catch((): CapabilityResult => ({ kind: 'unreachable' }))
    if (!this.disposed) this.set({ capabilities: r })
  }
}
