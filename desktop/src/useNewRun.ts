import { useEffect, useRef, useState } from 'react'
import { startRun, validateGraph, type ApiFailure } from './api'
import { getCapabilities, type CapabilityResult } from './capabilities'
import { openDocument, recentGraphs } from './editor/api'
import type { Definition } from './editor/types'
import { agentWorkspace, inputFields, inputLines } from './newRunModel'
import type { ChosenGraph, Diagnostic, RecentGraph } from './types'

// F2.5c: the state behind New run. The graph is addressed by grant only; its
// path stays in the main process (ADR-0021). Choosing a graph validates it and
// opens its document (for the declared inputs and node kinds); answers for a
// graph that is no longer chosen are dropped.

const unreachable = (e: unknown): ApiFailure => ({ message: e instanceof Error ? e.message : 'the engine could not be reached', diagnostics: [] })

/** Which option of the graph list is chosen: a recent graph by id, or a file chosen with the dialog. */
export type GraphSource = { kind: 'recent'; id: string } | { kind: 'file' }

export function useNewRun(initialGraph: ChosenGraph | undefined, onStarted: (runId: string) => void) {
  const [graph, setGraph] = useState<ChosenGraph | null>(initialGraph ?? null)
  const [source, setSource] = useState<GraphSource | null>(initialGraph ? { kind: 'file' } : null)
  const current = useRef(graph)
  const mounted = useRef(false)
  const [recent, setRecent] = useState<RecentGraph[]>([])
  const [def, setDef] = useState<Definition | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [validation, setValidation] = useState<{ status: string; diagnostics: Diagnostic[] } | null>(null)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)
  const [startError, setStartError] = useState<ApiFailure | null>(null)
  const [busy, setBusy] = useState(false)
  const [capabilities, setCapabilities] = useState<CapabilityResult | 'loading' | null>(null)

  const release = (g: ChosenGraph) => void window.agentplane.closeGraph(g.grant).catch(() => undefined)
  const isCurrent = (g: ChosenGraph) => current.current?.grant === g.grant

  async function inspect(g: ChosenGraph) {
    setValidation(null)
    setValidationError(null)
    setStartError(null)
    setDef(null)
    setValues({})
    setValidating(true)
    const [checked, opened] = await Promise.all([validateGraph(g.grant).catch(unreachable), openDocument(g.grant).catch(() => null)])
    if (!isCurrent(g)) return // another graph was chosen meanwhile
    setValidating(false)
    if ('message' in checked) setValidationError(checked.message)
    else setValidation(checked)
    if (opened?.ok && opened.doc.definition) setDef(opened.doc.definition)
  }

  function adopt(chosen: ChosenGraph, from: GraphSource) {
    const previous = current.current
    current.current = chosen
    setGraph(chosen)
    setSource(from)
    if (previous && previous.grant !== chosen.grant) release(previous)
    void inspect(chosen)
  }

  async function chooseFile() {
    const picked = await window.agentplane.openGraph()
    if ('canceled' in picked) return
    if (!picked.ok) {
      setStartError({ message: picked.message, diagnostics: [] })
      return
    }
    adopt({ grant: picked.grant, label: picked.label }, { kind: 'file' })
  }

  async function chooseRecent(r: RecentGraph) {
    const picked = await window.agentplane.openRecentGraph(r.id)
    if ('canceled' in picked) return
    if (!picked.ok) {
      setStartError({ message: picked.message, diagnostics: [] })
      return
    }
    adopt({ grant: picked.grant, label: picked.label }, { kind: 'recent', id: r.id })
  }

  async function run() {
    if (!graph) return
    setBusy(true)
    setStartError(null)
    const r = await startRun(graph.grant, inputLines(inputFields(def), values)).catch(unreachable)
    setBusy(false)
    if ('message' in r) setStartError(r)
    else onStarted(r.runId)
  }

  // Opened with a graph already chosen (Inbox "Open graph"): check it right away.
  useEffect(() => {
    if (initialGraph) void inspect(initialGraph)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialGraph])
  useEffect(() => {
    let live = true
    void recentGraphs().then((r) => live && setRecent(r))
    return () => {
      live = false
    }
  }, [])
  // The capability report is read once, and only for a graph with an Agent node.
  const needsAgent = agentWorkspace(def).agents > 0
  useEffect(() => {
    if (!needsAgent || capabilities !== null) return
    setCapabilities('loading')
    void getCapabilities().then((r) => mounted.current && setCapabilities(r))
  }, [needsAgent, capabilities])
  // Leaving the screen revokes the grant. StrictMode re-runs the effect synchronously, so only a real unmount releases.
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      queueMicrotask(() => {
        if (!mounted.current && current.current) release(current.current)
      })
    }
  }, [])

  const setValue = (name: string, value: string) => setValues((v) => ({ ...v, [name]: value }))
  return { graph, source, recent, def, values, setValue, validation, validationError, validating, startError, busy, capabilities, chooseFile, chooseRecent, run }
}
