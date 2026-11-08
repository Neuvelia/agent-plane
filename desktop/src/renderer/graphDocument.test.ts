import { describe, expect, it } from 'vitest'
import type { DocumentView } from '../preload/api'
import { GraphDocumentController, type GraphDocumentBridge, type GraphInspection, type GraphPreview } from './graphDocument'

const validGraph = { schema_version: 0, id: 'sample', units: [], inputs: [], nodes: [], edges: [], graph_input_bindings: [], outputs: [] }
const diagnostics = { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false }
const savedText = 'schema_version: 0\nid: sample\ninputs: {}\nnodes: {}\nedges: []\noutputs: {}\n'

function fixture() {
  let revision = 1
  let currentText = savedText
  let diskText = savedText
  const diskDigest = 'saved-digest'
  const calls: string[] = []
  let reformat = false
  const initial: DocumentView = {
    document_id: 'document', project_id: 'project', relative_path: 'sample.yaml',
    text: currentText, revision, disk_digest: diskDigest, validity: 'valid',
    semantic_version: 'sha256-v0:sample'
  }
  const inspection = (): GraphInspection => ({ revision, validity: currentText.startsWith('schema_version: 0') ? 'valid' : 'invalid',
    ...(currentText.startsWith('schema_version: 0') ? { graph: validGraph, layout: { version: 1, source: 'fallback', nodes: {} } } : {}),
    diagnostics })
  const document = (): DocumentView => ({ ...initial, text: currentText, revision,
    validity: inspection().validity, semantic_version: inspection().validity === 'valid' ? 'sha256-v0:sample' : undefined })
  const bridge: GraphDocumentBridge = {
    updateDocument: async (_id, expected, text) => {
      calls.push(`update:${expected}:${text}`)
      if (expected !== revision) throw Object.assign(new Error('revision conflict'), { code: 'conflict' })
      revision++
      currentText = text
      return document()
    },
    saveDocument: async (_id, expected, digest) => {
      calls.push(`save:${expected}:${digest}`)
      if (expected !== revision || diskText !== savedText) throw Object.assign(new Error('disk conflict'), { code: 'conflict' })
      diskText = currentText
      return document()
    },
    reloadDocument: async (_id, expected, discard) => {
      calls.push(`reload:${expected}:${discard}`)
      if (expected !== revision || (currentText !== diskText && !discard)) throw Object.assign(new Error('reload requires discard'), { code: 'conflict' })
      revision++
      currentText = diskText
      return document()
    },
    inspectDocument: async (_id, expected) => {
      calls.push(`inspect:${expected}`)
      if (expected !== revision) throw Object.assign(new Error('revision conflict'), { code: 'conflict' })
      return inspection()
    },
    previewDocumentEdit: async (_id, expected, operation): Promise<GraphPreview> => {
      calls.push(`preview:${expected}:${operation.kind}`)
      if (expected !== revision) throw Object.assign(new Error('revision conflict'), { code: 'conflict' })
      return { base_revision: revision, text: `${currentText}# visual edit\n`, validity: 'valid',
        diagnostics, reformat_required: reformat, changed_paths: ['nodes.plan'] }
    }
  }
  return { bridge, initial, calls, setExternalText: (text: string) => { diskText = text },
    setReformat: (value: boolean) => { reformat = value }, getDiskText: () => diskText,
    bumpRevision: () => { revision++ } }
}

describe('revisioned graph document controller', () => {
  it('keeps exact invalid YAML and the last valid canvas while blocking visual edits and Start', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.updateText('schema_version: [\n# exact draft\n')
    const state = controller.getSnapshot()
    expect(state.document.text).toBe('schema_version: [\n# exact draft\n')
    expect(state.dirty).toBe(true)
    expect(state.staleCanvas).toBe(true)
    expect(state.lastValid?.graph?.id).toBe('sample')
    expect(state.canVisualEdit).toBe(false)
    expect(state.canStart).toBe(false)
    await expect(controller.previewVisual({ kind: 'add_node', node_id: 'plan', node_type: 'agent' })).rejects.toMatchObject({ code: 'invalid_request' })
    expect(app.calls.some((call) => call.startsWith('preview:'))).toBe(false)
  })

  it('does not apply a reformat preview until it is explicitly accepted', async () => {
    const app = fixture()
    app.setReformat(true)
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.previewVisual({ kind: 'add_node', node_id: 'plan', node_type: 'agent' })
    expect(controller.getSnapshot().reformatPending).toBe(true)
    expect(app.calls.some((call) => call.startsWith('update:'))).toBe(false)
    await expect(controller.acceptPreview(false)).rejects.toMatchObject({ code: 'reformat_required' })
    expect(app.calls.some((call) => call.startsWith('update:'))).toBe(false)
    await controller.acceptPreview(true)
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# visual edit\n`)
    expect(controller.getSnapshot().reformatPending).toBe(false)
    expect(controller.getSnapshot().dirty).toBe(true)
  })

  it('cancels a full-text reformat preview without changing the YAML buffer', async () => {
    const app = fixture()
    app.setReformat(true)
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.previewVisual({ kind: 'add_node', node_id: 'plan', node_type: 'agent' })
    controller.discardPreview()
    expect(controller.getSnapshot()).toMatchObject({ preview: null, reformatPending: false,
      dirty: false, document: { text: savedText } })
    await expect(controller.acceptPreview(true)).rejects.toMatchObject({ code: 'conflict' })
    expect(app.calls.some((call) => call.startsWith('update:'))).toBe(false)
  })

  it('lets the user retry a visual delete after an explicit dependent-reference warning', async () => {
    const app = fixture()
    const preview = app.bridge.previewDocumentEdit
    app.bridge.previewDocumentEdit = async (...args) => {
      if (args[2].kind === 'delete_node' && !args[2].remove_references) {
        throw Object.assign(new Error('Node has dependent graph references; confirm their removal'), { code: 'conflict' })
      }
      return preview(...args)
    }
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await expect(controller.previewVisual({ kind: 'delete_node', node_id: 'plan', remove_references: false }))
      .rejects.toMatchObject({ code: 'conflict' })
    expect(controller.getSnapshot().externalConflict).toBe(false)
    expect(controller.getSnapshot().canVisualEdit).toBe(true)
    await expect(controller.previewVisual({ kind: 'delete_node', node_id: 'plan', remove_references: true }))
      .resolves.toMatchObject({ changed_paths: ['nodes.plan'] })
  })

  it('uses new revisions for undo and redo, without saving to disk', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.updateText(`${savedText}# one\n`)
    await controller.updateText(`${savedText}# two\n`)
    await controller.undo()
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# one\n`)
    await controller.undo()
    expect(controller.getSnapshot().document.text).toBe(savedText)
    expect(controller.getSnapshot().dirty).toBe(false)
    await controller.redo()
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# one\n`)
    expect(app.calls.filter((call) => call.startsWith('update:')).map((call) => call.split(':')[1])).toEqual(['1', '2', '3', '4', '5'])
    expect(app.calls.some((call) => call.startsWith('save:'))).toBe(false)
    expect(app.getDiskText()).toBe(savedText)
  })

  it('keeps an unsaved draft after an external save conflict and reloads only on explicit discard', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.updateText(`${savedText}# my draft\n`)
    app.setExternalText(`${savedText}# external\n`)
    await expect(controller.save()).rejects.toMatchObject({ code: 'conflict' })
    expect(controller.getSnapshot()).toMatchObject({ dirty: true, externalConflict: true })
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# my draft\n`)
    await expect(controller.reloadDisk(false)).rejects.toMatchObject({ code: 'conflict' })
    expect(app.calls.some((call) => call.startsWith('reload:'))).toBe(false)
    controller.keepDraft()
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# my draft\n`)
    await controller.reloadDisk(true)
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# external\n`)
    expect(controller.getSnapshot()).toMatchObject({ dirty: false, externalConflict: false, canUndo: false, canRedo: false })
  })

  it('leaves an obsolete visual preview unapplied after a text edit or revision conflict', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.previewVisual({ kind: 'add_node', node_id: 'plan', node_type: 'agent' })
    await controller.updateText(`${savedText}# yaml edit\n`)
    await expect(controller.acceptPreview(true)).rejects.toMatchObject({ code: 'conflict' })
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# yaml edit\n`)
    await controller.previewVisual({ kind: 'add_node', node_id: 'plan', node_type: 'agent' })
    app.bumpRevision()
    await expect(controller.acceptPreview(true)).rejects.toMatchObject({ code: 'conflict' })
    expect(controller.getSnapshot().externalConflict).toBe(true)
  })

  it('keeps the newest YAML keystrokes visible while earlier revision replies are pending', async () => {
    const app = fixture()
    const update = app.bridge.updateDocument
    let releaseFirst!: () => void
    const firstGate = new Promise<void>((resolve) => { releaseFirst = resolve })
    let calls = 0
    app.bridge.updateDocument = async (...args) => {
      if (calls++ === 0) await firstGate
      return update(...args)
    }
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    const first = controller.updateText(`${savedText}# a\n`)
    const second = controller.updateText(`${savedText}# ab\n`)
    expect(controller.getSnapshot()).toMatchObject({ draftText: `${savedText}# ab\n`, pendingUpdates: 2,
      canVisualEdit: false, canStart: false })
    releaseFirst()
    await first
    expect(controller.getSnapshot()).toMatchObject({ document: { text: `${savedText}# a\n` },
      draftText: `${savedText}# ab\n`, pendingUpdates: 1 })
    await second
    expect(controller.getSnapshot()).toMatchObject({ document: { text: `${savedText}# ab\n` },
      draftText: `${savedText}# ab\n`, pendingUpdates: 0 })
  })

  it('does not let an undo reply replace text typed after the undo began', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.updateText(`${savedText}# first\n`)
    const update = app.bridge.updateDocument
    let releaseUndo!: () => void
    const undoGate = new Promise<void>((resolve) => { releaseUndo = resolve })
    let calls = 0
    app.bridge.updateDocument = async (...args) => {
      if (calls++ === 0) await undoGate
      return update(...args)
    }
    const undo = controller.undo()
    const typed = controller.updateText(`${savedText}# latest\n`)
    releaseUndo()
    await undo
    expect(controller.getSnapshot().draftText).toBe(`${savedText}# latest\n`)
    await typed
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# latest\n`)
  })

  it('still marks the graph dirty when a failed revert leaves unsaved service text', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.updateText(`${savedText}# unsaved\n`)
    app.bridge.updateDocument = async () => { throw Object.assign(new Error('service unavailable'), { code: 'unavailable' }) }
    await expect(controller.updateText(savedText)).rejects.toMatchObject({ code: 'unavailable' })
    expect(controller.getSnapshot()).toMatchObject({ draftText: savedText,
      document: { text: `${savedText}# unsaved\n` }, dirty: true, canStart: false })
  })

  it('does not undo or redo over an unsent draft after a failed document update', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    await controller.updateText(`${savedText}# first\n`)
    await controller.undo()
    const update = app.bridge.updateDocument
    app.bridge.updateDocument = async (...args) => {
      if (args[2].includes('# unsent')) throw Object.assign(new Error('service unavailable'), { code: 'unavailable' })
      return update(...args)
    }
    await expect(controller.updateText(`${savedText}# unsent\n`)).rejects.toMatchObject({ code: 'unavailable' })
    expect(controller.getSnapshot()).toMatchObject({ canUndo: false, canRedo: false,
      draftText: `${savedText}# unsent\n`, document: { text: savedText } })
    const before = app.calls.filter((call) => call.startsWith('update:')).length
    await expect(controller.undo()).rejects.toMatchObject({ code: 'conflict' })
    await expect(controller.redo()).rejects.toMatchObject({ code: 'conflict' })
    expect(app.calls.filter((call) => call.startsWith('update:'))).toHaveLength(before)
    expect(controller.getSnapshot().draftText).toBe(`${savedText}# unsent\n`)
  })

  it('keeps a bounded undo history and clears redo after a new edit', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    for (let index = 0; index < 40; index++) await controller.updateText(`${savedText}# ${index}\n`)
    for (let index = 0; index < 32; index++) await controller.undo()
    expect(controller.getSnapshot()).toMatchObject({ canUndo: false, canRedo: true,
      document: { text: `${savedText}# 7\n` } })
    await controller.redo()
    expect(controller.getSnapshot().document.text).toBe(`${savedText}# 8\n`)
    await controller.updateText(`${savedText}# new\n`)
    expect(controller.getSnapshot().canRedo).toBe(false)
  })

  it('drops old large snapshots before undo history exceeds the byte budget', async () => {
    const app = fixture()
    const controller = await GraphDocumentController.open(app.bridge, app.initial)
    const large = 'x'.repeat(2 << 20)
    for (let index = 0; index < 10; index++) await controller.updateText(`${savedText}# ${index}\n${large}`)
    let undos = 0
    while (controller.getSnapshot().canUndo) { await controller.undo(); undos++ }
    expect(undos).toBeGreaterThan(0)
    expect(undos).toBeLessThan(8)
  })
})
