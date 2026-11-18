import { useCallback, useEffect, useRef, useState } from 'react'
import type { GraphGrantResult, RecentGraph } from '../types'
import type { Op, OpenResponse, OpResult } from './types'
import { analyzed, edited, fromNew, fromOpen, isDirty, newGraphText, redo as redoDoc, reloaded, saved, type as typeDoc, undo as undoDoc, validating, validationFailed, withConflict, type DocState } from './doc'
import { editDocument, openDocument, runGrant, recentGraphs, saveDocument, statDocument, validateText } from './api'

/** How long typing pauses before the text is validated (spec F2.1, requirement 12). */
export const VALIDATE_DELAY_MS = 300

export type EditorError = { kind: 'open' | 'save' | 'edit' | 'run'; message: string }

export type ApplyResult = { ok: true; results: OpResult[] } | { ok: false; message: string }

/** Something the person asked for that would leave unsaved changes behind. */
export interface Leave {
  run: () => Promise<void>
}

export interface GraphDocument {
  doc: DocState | null
  busy: 'opening' | 'saving' | 'starting' | null
  error: EditorError | null
  recent: RecentGraph[]
  leave: Leave | null
  open: () => void
  /** New graph through the native save dialog; `draft` builds the text from the chosen file name (a template, F2.12), default blank. */
  create: (draft?: (label: string) => string | null) => void
  openRecent: (id: string) => void
  close: () => void
  type: (text: string) => void
  /** Sends operations for the text on screen; a success replaces the draft as one undo step. */
  applyOps: (ops: Op[], opts?: { quiet?: boolean }) => Promise<ApplyResult>
  /**
   * Asks the engine what operations would produce, without adopting the answer:
   * the draft, history and file are unchanged (port-edit previews, binding checks).
   */
  previewOps: (ops: Op[]) => Promise<{ ok: true; doc: OpenResponse } | { ok: false; message: string }>
  /** Validate the draft now instead of after the typing pause. */
  revalidate: () => void
  undo: () => void
  redo: () => void
  save: () => Promise<void>
  /** Saves a changed draft, then starts a run of the saved file. Returns the run id, or null when nothing started. */
  saveAndRun: (inputs: string[]) => Promise<string | null>
  keepMine: () => Promise<void>
  reload: () => Promise<void>
  dismissError: () => void
  /** Leave prompt answers. */
  saveAndLeave: () => Promise<void>
  discardAndLeave: () => Promise<void>
  cancelLeave: () => void
}

// The editor's document lives above the screen, so it survives navigating to
// another screen and back. The file is the truth: this hook holds text and asks
// the sidecar what it means.
export function useGraphDocument(): GraphDocument {
  const [doc, setDoc] = useState<DocState | null>(null)
  const [busy, setBusy] = useState<GraphDocument['busy']>(null)
  const [error, setError] = useState<EditorError | null>(null)
  const [recent, setRecent] = useState<RecentGraph[]>([])
  const [leave, setLeave] = useState<Leave | null>(null)
  const docRef = useRef(doc)
  docRef.current = doc
  const validateSeq = useRef(0)
  const [tick, setTick] = useState(0)

  const refreshRecent = useCallback(() => void recentGraphs().then(setRecent), [])
  useEffect(refreshRecent, [refreshRecent])

  const closeGrant = (grant: string) => void window.agentplane.closeGraph(grant).catch(() => undefined)

  // Replace the open document with a freshly chosen one; the old grant is revoked
  // only once the new one is in place.
  const adopt = useCallback(
    async (result: GraphGrantResult, kind: 'open' | 'new', draft: (label: string) => string | null = newGraphText) => {
      if ('canceled' in result) return
      if (!result.ok) {
        setError({ kind: 'open', message: result.message })
        return
      }
      const previous = docRef.current
      if (kind === 'new') {
        const text = draft(result.label)
        if (text === null) {
          closeGrant(result.grant)
          setError({ kind: 'open', message: 'the template is unavailable' })
          return
        }
        setDoc(fromNew(result.grant, result.label, text))
      } else {
        setBusy('opening')
        const opened = await openDocument(result.grant)
        setBusy(null)
        if (!opened.ok) {
          closeGrant(result.grant)
          setError({ kind: 'open', message: opened.message })
          return
        }
        setDoc(fromOpen(result.grant, opened.doc))
      }
      setError(null)
      if (previous) closeGrant(previous.grant)
      refreshRecent()
    },
    [refreshRecent],
  )

  // Anything that would drop unsaved text asks first.
  const guarded = useCallback((action: () => Promise<void>) => {
    const current = docRef.current
    if (current && isDirty(current)) setLeave({ run: action })
    else void action()
  }, [])

  const open = useCallback(() => guarded(async () => adopt(await window.agentplane.openGraph(), 'open')), [adopt, guarded])
  const create = useCallback((draft?: (label: string) => string | null) => guarded(async () => adopt(await window.agentplane.newGraph(), 'new', draft)), [adopt, guarded])
  const openRecent = useCallback((id: string) => guarded(async () => adopt(await window.agentplane.openRecentGraph(id), 'open')), [adopt, guarded])
  const close = useCallback(
    () =>
      guarded(async () => {
        const current = docRef.current
        if (current) closeGrant(current.grant)
        setDoc(null)
        setError(null)
      }),
    [guarded],
  )

  const type = useCallback((text: string) => setDoc((d) => (d ? typeDoc(d, text, Date.now()) : d)), [])
  const applyOps = useCallback(async (ops: Op[], opts?: { quiet?: boolean }): Promise<ApplyResult> => {
    const current = docRef.current
    if (!current) return { ok: false, message: 'no graph is open' }
    const text = current.draft
    const r = await editDocument(current.grant, text, ops)
    if (!r.ok) {
      const message = r.detail?.suggestion ? `${r.detail.message} (${r.detail.suggestion})` : r.message
      if (!opts?.quiet) setError({ kind: 'edit', message })
      return { ok: false, message }
    }
    // The person kept typing while the engine worked: their text wins.
    if (docRef.current?.grant !== current.grant || docRef.current.draft !== text) return { ok: false, message: 'the text changed while the operation ran' }
    setError(null)
    setDoc((d) => (d ? edited(d, r.doc) : d))
    return { ok: true, results: r.doc.results ?? [] }
  }, [])
  const previewOps = useCallback(async (ops: Op[]): Promise<{ ok: true; doc: OpenResponse } | { ok: false; message: string }> => {
    const current = docRef.current
    if (!current) return { ok: false, message: 'no graph is open' }
    const r = await editDocument(current.grant, current.draft, ops)
    if (!r.ok) return { ok: false, message: r.detail?.suggestion ? `${r.detail.message} (${r.detail.suggestion})` : r.message }
    return { ok: true, doc: r.doc }
  }, [])
  const revalidate = useCallback(() => {
    setDoc((d) => (d ? { ...d, analysis: { ...d.analysis, stale: true } } : d))
    setTick((n) => n + 1)
  }, [])
  const undo = useCallback(() => setDoc((d) => (d ? undoDoc(d) : d)), [])
  const redo = useCallback(() => setDoc((d) => (d ? redoDoc(d) : d)), [])

  // Saving sends the draft with the hash of the file as last seen. A refusal is
  // never silent: it becomes a conflict banner or an error.
  const saveWith = useCallback(
    async (baseHash: string | null): Promise<boolean> => {
      const current = docRef.current
      if (!current) return false
      const text = current.draft
      setBusy('saving')
      const outcome = await saveDocument(current.grant, baseHash, text)
      setBusy(null)
      if (outcome.kind === 'saved') {
        setError(null)
        const wasNew = current.isNew
        const hash = outcome.hash
        setDoc((d) => (d && d.grant === current.grant ? saved(d, text, hash) : d))
        if (wasNew) void window.agentplane.touchRecent(current.grant).then(refreshRecent)
        return true
      }
      if (outcome.kind === 'conflict') setDoc((d) => (d ? withConflict(d, { kind: 'changed', currentHash: outcome.currentHash }) : d))
      else if (outcome.kind === 'exists') setDoc((d) => (d ? withConflict(d, { kind: 'exists' }) : d))
      else if (outcome.kind === 'gone') setDoc((d) => (d ? withConflict(d, { kind: 'gone' }) : d))
      else setError({ kind: 'save', message: outcome.message })
      return false
    },
    [refreshRecent],
  )

  const save = useCallback(async () => {
    const current = docRef.current
    if (current) await saveWith(current.savedHash)
  }, [saveWith])

  const saveAndRun = useCallback(
    async (inputs: string[]): Promise<string | null> => {
      const current = docRef.current
      if (!current) return null
      // A refused save (conflict, gone, error) stops here: nothing runs from a file the person did not mean.
      if (current.isNew || isDirty(current)) {
        if (!(await saveWith(current.savedHash))) return null
      }
      setBusy('starting')
      const r = await runGrant(current.grant, inputs)
      setBusy(null)
      if (!r.ok) {
        setError({ kind: 'run', message: r.message })
        return null
      }
      return r.runId
    },
    [saveWith],
  )

  // "Keep mine": overwrite what is on disk now, knowing its hash. If the file is
  // gone (or someone created it), the choice is to create or replace it.
  const keepMine = useCallback(async () => {
    const c = docRef.current?.conflict
    if (!c) return
    if (c.kind === 'changed') await saveWith(c.currentHash)
    else if (c.kind === 'gone') await saveWith(null)
    else {
      const stat = await statDocument(docRef.current!.grant)
      await saveWith(stat?.exists ? (stat.hash ?? null) : null)
    }
  }, [saveWith])

  const reload = useCallback(async () => {
    const current = docRef.current
    if (!current) return
    setBusy('opening')
    const opened = await openDocument(current.grant)
    setBusy(null)
    if (!opened.ok) {
      setError({ kind: 'open', message: opened.message })
      return
    }
    setError(null)
    setDoc((d) => (d && d.grant === current.grant ? reloaded(d, opened.doc) : d))
  }, [])

  const dismissError = useCallback(() => setError(null), [])

  const saveAndLeave = useCallback(async () => {
    const pending = leave
    await save()
    if (docRef.current && !isDirty(docRef.current) && pending) {
      setLeave(null)
      await pending.run()
    }
  }, [leave, save])
  const discardAndLeave = useCallback(async () => {
    const pending = leave
    setLeave(null)
    if (pending) await pending.run()
  }, [leave])
  const cancelLeave = useCallback(() => setLeave(null), [])

  // Validate the draft after a pause in typing. Answers for older text are ignored.
  const draft = doc?.draft
  const grant = doc?.grant
  const stale = doc?.analysis.stale
  useEffect(() => {
    if (draft === undefined || !stale) return
    const seq = ++validateSeq.current
    const timer = setTimeout(async () => {
      setDoc((d) => (d && d.draft === draft ? validating(d) : d))
      const result = await validateText(draft)
      if (seq !== validateSeq.current) return
      setDoc((d) => (d ? (result ? analyzed(d, draft, result) : validationFailed(d, draft)) : d))
    }, VALIDATE_DELAY_MS)
    return () => clearTimeout(timer)
  }, [draft, grant, stale, tick])

  // On window focus, look at the file: a clean draft follows the file silently, a
  // dirty one is told about the change and chooses (spec F2.1, requirement 18).
  useEffect(() => {
    if (!grant) return
    const onFocus = async () => {
      const current = docRef.current
      if (!current || current.grant !== grant) return
      const stat = await statDocument(grant)
      if (!stat) return
      const now = docRef.current
      if (!now || now.grant !== grant) return
      const changed = stat.exists ? stat.hash !== now.savedHash : !now.isNew
      if (!changed) return
      if (stat.exists && !isDirty(now)) {
        const opened = await openDocument(grant)
        if (opened.ok) setDoc((d) => (d && d.grant === grant && !isDirty(d) ? reloaded(d, opened.doc) : d))
        return
      }
      setDoc((d) => (d && d.grant === grant ? withConflict(d, stat.exists ? (now.isNew ? { kind: 'exists' } : { kind: 'changed', currentHash: stat.hash ?? '' }) : { kind: 'gone' }) : d))
    }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [grant])

  return { doc, busy, error, recent, leave, open, create, openRecent, close, type, applyOps, previewOps, revalidate, undo, redo, save, saveAndRun, keepMine, reload, dismissError, saveAndLeave, discardAndLeave, cancelLeave }
}

