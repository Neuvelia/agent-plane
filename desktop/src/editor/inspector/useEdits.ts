import { useCallback, useEffect, useState } from 'react'
import type { Definition, Op } from '../types'
import type { GraphDocument } from '../useGraphDocument'
import { describeChange, type Change } from './model'

// How the inspector changes the file (F2.2 requirement 8): every committed
// change is one operation batch through the document's applyOps, so the YAML
// view, the canvas and undo follow it. A refusal is shown where the change was
// made, keyed by a `where` string, never as a silent revert.

export interface Pending {
  ops: Op[]
  changes: Change[]
  where: string
  /** Settles the intend() call that asked: the apply result, or false when cancelled. */
  settle: (ok: boolean) => void
}

export interface Edits {
  /** Sends the batch; true when the engine applied it. */
  apply: (ops: Op[], where: string) => Promise<boolean>
  /** Previews the batch first; applies it at once when it touches nothing but the edited port, else asks. True once applied; false when refused or cancelled. */
  intend: (ops: Op[], where: string) => Promise<boolean>
  refusal: (where: string) => string | undefined
  pending: Pending | null
  confirm: () => Promise<void>
  cancel: () => void
  busy: boolean
}

export function useEdits(g: GraphDocument, def: Definition | undefined, nodeId: string | null): Edits {
  const [refusals, setRefusals] = useState<Record<string, string>>({})
  const [pending, setPending] = useState<Pending | null>(null)
  const [busy, setBusy] = useState(false)
  // A different node starts clean.
  useEffect(() => {
    setRefusals({})
    setPending((p) => {
      p?.settle(false)
      return null
    })
  }, [nodeId])

  const note = (where: string, message?: string) =>
    setRefusals((r) => {
      const next = { ...r }
      if (message) next[where] = message
      else delete next[where]
      return next
    })

  const apply = useCallback(
    async (ops: Op[], where: string) => {
      setBusy(true)
      const r = await g.applyOps(ops, { quiet: true })
      setBusy(false)
      note(where, r.ok ? undefined : r.message)
      return r.ok
    },
    [g],
  )

  const intend = useCallback(
    async (ops: Op[], where: string) => {
      if (!def) return false
      setBusy(true)
      const r = await g.previewOps(ops)
      setBusy(false)
      if (!r.ok) {
        note(where, r.message)
        return false
      }
      const after = r.doc.definition
      const changes = after ? describeChange(def, after) : []
      if (changes.some((c) => c.what !== 'port')) {
        note(where)
        return new Promise<boolean>((settle) =>
          setPending((old) => {
            old?.settle(false)
            return { ops, changes, where, settle }
          }),
        )
      }
      return apply(ops, where)
    },
    [apply, def, g],
  )

  const confirm = useCallback(async () => {
    const p = pending
    setPending(null)
    if (p) p.settle(await apply(p.ops, p.where))
  }, [apply, pending])

  const cancel = () => {
    pending?.settle(false)
    setPending(null)
  }

  return { apply, intend, refusal: (where) => refusals[where], pending, confirm, cancel, busy }
}
