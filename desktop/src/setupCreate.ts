import { useEffect, useState } from 'react'
import type { DraftMaker } from './editor/TemplatePicker'
import type { GraphDocument } from './editor/useGraphDocument'

// Setup's "Create and open" (ADR-0046). Creating asks for a file first (the
// native save dialog), so setup is left only once that has led somewhere:
// a new document opens the editor and completes setup; a cancelled dialog
// leaves the person on setup, not on an empty editor.

/** What the document looked like when the create began. */
export interface CreateStart {
  grant: string | null
  error: unknown
}

/** What the document looks like now. */
export interface CreateNow {
  grant: string | null
  error: unknown
  leave: boolean
}

/**
 * - `created`: a new document is open; complete setup and show it.
 * - `failed`: the create reported an error; show the editor, where it is explained.
 * - `ask`: unsaved text must be saved or discarded first; show the editor's prompt and keep watching.
 * - `pending`: nothing yet (the dialog is open, or it was cancelled).
 */
export type CreateOutcome = 'created' | 'failed' | 'ask' | 'pending'

export function createOutcome(start: CreateStart, now: CreateNow): CreateOutcome {
  if (now.grant !== null && now.grant !== start.grant) return 'created'
  if (now.error !== null && now.error !== start.error) return 'failed'
  return now.leave ? 'ask' : 'pending'
}

type Doc = Pick<GraphDocument, 'doc' | 'error' | 'leave' | 'create'>

/** Starts a create and reports where it led; returns the function setup calls. */
export function useSetupCreate(g: Doc, onCreated: () => void, onShowEditor: () => void): (make: DraftMaker | undefined) => void {
  const [start, setStart] = useState<CreateStart | null>(null)
  const grant = g.doc?.grant ?? null
  const asking = g.leave !== null
  useEffect(() => {
    if (!start) return
    const outcome = createOutcome(start, { grant, error: g.error, leave: asking })
    if (outcome === 'pending') return
    if (outcome === 'ask') return onShowEditor()
    setStart(null)
    if (outcome === 'created') onCreated()
    else onShowEditor()
  }, [start, grant, g.error, asking, onCreated, onShowEditor])
  return (make) => {
    setStart({ grant, error: g.error })
    g.create(make)
  }
}
