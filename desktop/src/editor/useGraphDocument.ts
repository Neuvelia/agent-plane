import { useCallback, useEffect, useRef, useState } from 'react'
import type { GraphGrantResult, RecentGraph } from '../types'
import type { Op, OpResult } from './types'
import { analyzed, edited, fromNew, fromOpen, isDirty, newGraphText, redo as redoDoc, reloaded, saved, type as typeDoc, undo as undoDoc, validating, withConflict, type DocState } from './doc'
import { editDocument, openDocument, recentGraphs, saveDocument, statDocument, validateText } from './api'

/** How long typing pauses before the text is validated (spec F2.1, requirement 12). */
export const VALIDATE_DELAY_MS = 300

export type EditorError = { kind: 'open' | 'save' | 'edit'; message: string }

export type ApplyResult = { ok: true; results: OpResult[] } | { ok: false; message: string }

/** Something the person asked for that would leave unsaved changes behind. */
export interface Leave {
  run: () => Promise<void>
}

export interface GraphDocument {
  doc: DocState | null
  busy: 'opening' | 'saving' | null
  error: EditorError | null
  recent: RecentGraph[]
  leave: Leave | null
  open: () => void
  create: () => void
  openRecent: (id: string) => void
  close: () => void
  type: (text: string) => void
  /** Sends operations for the text on screen; a success replaces the draft as one undo step. */
  applyOps: (ops: Op[], opts?: { quiet?: boolean }) => Promise<ApplyResult>
  /** Validate the draft now instead of after the typing pause. */
  revalidate: () => void
  undo: () => void
  redo: () => void
  save: () => Promise<void>
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

  const refreshRecent = useCallback(() => void recentGraphs().then(setRecent), [])
  useEffect(refreshRecent, [refreshRecent])

  const closeGrant = (grant: string) => void window.agentplane.closeGraph(grant).catch(() => undefined)

  // Replace the open document with a freshly chosen one; the old grant is revoked
  // only once the new one is in place.
  const adopt = useCallback(
    async (result: GraphGrantResult, kind: 'open' | 'new') => {
      if ('canceled' in result) return
      if (!result.ok) {
        setError({ kind: 'open', message: result.message })
        return
      }
      const previous = docRef.current
      if (kind === 'new') {
        setDoc(fromNew(result.grant, result.label, newGraphText(result.label)))
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
  const create = useCallback(() => guarded(async () => adopt(await window.agentplane.newGraph(), 'new')), [adopt, guarded])
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
  const revalidate = useCallback(() => setDoc((d) => (d ? { ...d, analysis: { ...d.analysis, stale: true } } : d)), [])
  const undo = useCallback(() => setDoc((d) => (d ? undoDoc(d) : d)), [])
  const redo = useCallback(() => setDoc((d) => (d ? redoDoc(d) : d)), [])

  // Saving sends the draft with the hash of the file as last seen. A refusal is
  // never silent: it becomes a conflict banner or an error.
  const saveWith = useCallback(
    async (baseHash: string | null) => {
      const current = docRef.current
      if (!current) return
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
        return
      }
      if (outcome.kind === 'conflict') setDoc((d) => (d ? withConflict(d, { kind: 'changed', currentHash: outcome.currentHash }) : d))
      else if (outcome.kind === 'exists') setDoc((d) => (d ? withConflict(d, { kind: 'exists' }) : d))
      else if (outcome.kind === 'gone') setDoc((d) => (d ? withConflict(d, { kind: 'gone' }) : d))
      else setError({ kind: 'save', message: outcome.message })
    },
    [refreshRecent],
  )

  const save = useCallback(async () => {
    const current = docRef.current
    if (current) await saveWith(current.savedHash)
  }, [saveWith])

  // "Keep mine": overwrite what is on disk now, knowing its hash. If the file is
  // gone (or someone created it), the choice is to create or replace it.
  const keepMine = useCallback(async () => {
    const c = docRef.current?.conflict
    if (!c) return
    if (c.kind === 'changed') return saveWith(c.currentHash)
    if (c.kind === 'gone') return saveWith(null)
    const stat = await statDocument(docRef.current!.grant)
    return saveWith(stat?.exists ? (stat.hash ?? null) : null)
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
      if (seq !== validateSeq.current || !result) return
      setDoc((d) => (d ? analyzed(d, draft, result) : d))
    }, VALIDATE_DELAY_MS)
    return () => clearTimeout(timer)
  }, [draft, grant, stale])

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

  return { doc, busy, error, recent, leave, open, create, openRecent, close, type, applyOps, revalidate, undo, redo, save, keepMine, reload, dismissError, saveAndLeave, discardAndLeave, cancelLeave }
}

