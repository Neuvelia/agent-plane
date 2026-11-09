import { describe, expect, it } from 'vitest'
import { canRestoreDraft, editorIsStale, putEditorSession, type EditorSession } from './editorSessions'
import type { DocumentView } from '../preload/api'
import type { GraphDocumentController } from './graphDocument'

const document: DocumentView = { document_id: 'old-doc', project_id: 'project-a', relative_path: 'graph.yaml',
  text: 'saved', revision: 3, disk_digest: 'digest-a', validity: 'valid' }

function session(projectID: string, dirty: boolean, generation = 1): EditorSession {
  return { projectID, path: 'graph.yaml', serviceGeneration: generation,
    controller: { getSnapshot: () => ({ document: { ...document, project_id: projectID }, draftText: dirty ? 'draft' : 'saved', dirty, pendingUpdates: 0 }) } as GraphDocumentController }
}

describe('project editor sessions', () => {
  it('keeps an unsaved draft when another project opens and rejects replacing its controller', () => {
    const first = session('project-a', true)
    const second = session('project-b', false)
    const sessions = putEditorSession(putEditorSession({}, first), second)
    expect(sessions['project-a']).toBe(first)
    expect(sessions['project-b']).toBe(second)
    expect(() => putEditorSession(sessions, session('project-a', false))).toThrow(/unsaved draft/i)
  })

  it('marks old document identities stale after a sidecar generation change', () => {
    const old = session('project-a', true, 1)
    expect(editorIsStale(old, 1)).toBe(false)
    expect(editorIsStale(old, 2)).toBe(true)
  })

  it('restores an old draft only onto the exact saved file identity and digest', () => {
    const old = session('project-a', true)
    expect(canRestoreDraft(old, { ...document, document_id: 'new-doc' })).toBe(true)
    expect(canRestoreDraft(old, { ...document, disk_digest: 'changed' })).toBe(false)
    expect(canRestoreDraft(old, { ...document, relative_path: 'other.yaml' })).toBe(false)
    expect(canRestoreDraft(old, { ...document, project_id: 'project-b' })).toBe(false)
  })
})
