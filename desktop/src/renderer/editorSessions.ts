import type { DocumentView } from '../preload/api'
import type { GraphDocumentController } from './graphDocument'

// One editor session per project. A session owns the draft controller and
// remembers which sidecar generation issued its document identity, so a
// restart can be told apart from an ordinary refresh.
export type EditorSession = {
  projectID: string
  path: string
  serviceGeneration: number
  controller: GraphDocumentController
}

export type EditorSessions = Record<string, EditorSession>

function hasDraft(session: EditorSession): boolean {
  return session.controller.getSnapshot().dirty
}

// Never replaces a session that holds an unsaved draft with a different one:
// the caller must save, restore or explicitly discard first.
export function putEditorSession(sessions: EditorSessions, next: EditorSession): EditorSessions {
  const current = sessions[next.projectID]
  if (current && current !== next && hasDraft(current)) {
    throw new Error(`Project ${next.projectID} has an unsaved draft for ${current.path}; save or discard it before opening another graph.`)
  }
  return { ...sessions, [next.projectID]: next }
}

export function discardEditorSession(sessions: EditorSessions, projectID: string): EditorSessions {
  const { [projectID]: _removed, ...rest } = sessions
  return rest
}

// Document IDs are only valid inside the sidecar generation that issued them.
export function editorIsStale(session: EditorSession, currentGeneration: number): boolean {
  return session.serviceGeneration !== currentGeneration
}

// A draft may be re-applied only to the same project and path whose on-disk
// digest still matches the one the draft was based on. Otherwise the draft is
// preserved for copying rather than merged into a changed file.
export function canRestoreDraft(old: EditorSession, reopened: DocumentView): boolean {
  const base = old.controller.getSnapshot().document
  return reopened.project_id === old.projectID && reopened.relative_path === old.path && reopened.disk_digest === base.disk_digest
}
