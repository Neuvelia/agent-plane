import type { DiagnosticSummary, DocumentView } from '../preload/api'

export type GraphPort = { name: string; type: string; required: boolean }
export type GraphNode = {
  id: string; type: string; inputs: GraphPort[]; outputs: GraphPort[]
  loop_max_attempts?: number; needs_configuration: boolean
}
export type GraphProjection = {
  schema_version: number; id: string; units: string[]; inputs: GraphPort[]
  nodes: GraphNode[]; edges: { from: string; to: string }[]
  graph_input_bindings: { from: string; to: string }[]
  outputs: { name: string; from: string }[]
}
export type GraphLayout = {
  version: number; source: 'fallback' | 'stored' | 'unsupported_version'
  nodes: Record<string, { x: number; y: number }>
}
export type GraphInspection = {
  revision: number; validity: DocumentView['validity']; diagnostics: DiagnosticSummary
  graph?: GraphProjection; layout?: GraphLayout
}
export type GraphEditOperation =
  | { kind: 'add_node'; node_id: string; node_type: 'shell' | 'agent' | 'gate' }
  | { kind: 'delete_node'; node_id: string; remove_references: boolean }
  | { kind: 'set_layout'; node_id: string; x: number; y: number; replace_unknown_layout?: boolean }
  | { kind: 'remove_layout'; node_id: string; replace_unknown_layout?: boolean }
export type GraphPreview = {
  base_revision: number; text: string; validity: DocumentView['validity']
  diagnostics: DiagnosticSummary; reformat_required: boolean; changed_paths: string[]
}

export type GraphDocumentBridge = {
  updateDocument(documentID: string, expectedRevision: number, text: string): Promise<DocumentView>
  saveDocument(documentID: string, expectedRevision: number, expectedDiskDigest: string, acceptReformat: boolean): Promise<DocumentView>
  reloadDocument(documentID: string, expectedRevision: number, discardDraft: boolean): Promise<DocumentView>
  inspectDocument(documentID: string, revision: number): Promise<GraphInspection>
  previewDocumentEdit(documentID: string, revision: number, operation: GraphEditOperation): Promise<GraphPreview>
}

export type GraphDocumentState = {
  document: DocumentView
  draftText: string
  pendingUpdates: number
  inspection: GraphInspection | null
  lastValid: GraphInspection | null
  preview: GraphPreview | null
  dirty: boolean
  staleCanvas: boolean
  canVisualEdit: boolean
  canStart: boolean
  canUndo: boolean
  canRedo: boolean
  externalConflict: boolean
  reformatPending: boolean
  error: string | null
}

type CodedError = Error & { code: string }
type TextSnapshot = { text: string; bytes: number }
const maxHistorySnapshots = 32
const maxHistoryBytes = 16 << 20
const encoder = new TextEncoder()
function snapshot(text: string): TextSnapshot { return { text, bytes: encoder.encode(text).byteLength } }
function codedError(code: string, message: string): CodedError {
  return Object.assign(new Error(message), { code })
}
function isConflict(cause: unknown): boolean {
  return typeof cause === 'object' && cause !== null && 'code' in cause && cause.code === 'conflict'
}
function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : 'Graph document operation unavailable'
}

// One queue keeps each Go revision check adjacent to its corresponding local
// state transition. Rapid YAML edits cannot overtake an earlier update.
export class GraphDocumentController {
  private tail: Promise<void> = Promise.resolve()
  private listeners = new Set<() => void>()
  private undoText: TextSnapshot[] = []
  private redoText: TextSnapshot[] = []
  private savedText: string
  private savedDigest: string
  private state: GraphDocumentState

  private constructor(private readonly api: GraphDocumentBridge, document: DocumentView, inspection: GraphInspection) {
    this.savedText = document.text
    this.savedDigest = document.disk_digest
    this.state = this.derive({ document, draftText: document.text, pendingUpdates: 0,
      inspection, lastValid: inspection.validity === 'valid' ? inspection : null,
      preview: null, externalConflict: false, error: null })
  }

  static async open(api: GraphDocumentBridge, document: DocumentView): Promise<GraphDocumentController> {
    const inspection = await api.inspectDocument(document.document_id, document.revision)
    if (inspection.revision !== document.revision || inspection.validity !== document.validity) {
      throw codedError('unavailable', 'Graph inspection does not match the selected document revision')
    }
    return new GraphDocumentController(api, document, inspection)
  }

  getSnapshot = (): GraphDocumentState => this.state

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }

  private derive(base: Pick<GraphDocumentState, 'document' | 'draftText' | 'pendingUpdates' | 'inspection' | 'lastValid' | 'preview' | 'externalConflict' | 'error'>): GraphDocumentState {
    const current = base.inspection?.revision === base.document.revision ? base.inspection : null
    const valid = base.pendingUpdates === 0 && base.draftText === base.document.text &&
      base.document.validity === 'valid' && current?.validity === 'valid' && !!current.graph
    const dirty = base.draftText !== this.savedText || base.document.text !== this.savedText
    const historyReady = base.pendingUpdates === 0 && base.draftText === base.document.text && !base.externalConflict
    return { ...base, inspection: current, dirty, staleCanvas: !valid && !!base.lastValid,
      canVisualEdit: valid && !base.externalConflict, canStart: valid && !dirty && !base.externalConflict,
      canUndo: historyReady && this.undoText.length > 0,
      canRedo: historyReady && this.redoText.length > 0,
      reformatPending: base.preview?.reformat_required === true }
  }

  private publish(base: Pick<GraphDocumentState, 'document' | 'draftText' | 'pendingUpdates' | 'inspection' | 'lastValid' | 'preview' | 'externalConflict' | 'error'>): void {
    this.state = this.derive(base)
    for (const listener of this.listeners) listener()
  }

  private queue<T>(work: () => Promise<T>): Promise<T> {
    const result = this.tail.then(work, work)
    this.tail = result.then(() => undefined, () => undefined)
    return result
  }

  private trimHistory(): void {
    let bytes = 0
    for (const item of [...this.undoText, ...this.redoText]) bytes += item.bytes
    while (this.undoText.length + this.redoText.length > maxHistorySnapshots || bytes > maxHistoryBytes) {
      const oldest = this.undoText.length > 0 ? this.undoText.shift() : this.redoText.shift()
      bytes -= oldest?.bytes ?? 0
    }
  }

  private fail(cause: unknown, markExternalConflict = true): never {
    this.publish({ ...this.state, preview: null,
      externalConflict: this.state.externalConflict || (markExternalConflict && isConflict(cause)),
      error: errorMessage(cause) })
    throw cause
  }

  private async applyText(text: string, history: 'new' | 'undo' | 'redo', preserveDraft = false): Promise<void> {
    const before = this.state.document
    if (text === before.text) return
    let updated: DocumentView
    try {
      updated = await this.api.updateDocument(before.document_id, before.revision, text)
      if (updated.document_id !== before.document_id || updated.revision !== before.revision + 1 || updated.text !== text) {
        throw codedError('unavailable', 'Graph update response does not match the submitted revision')
      }
    } catch (cause) { this.fail(cause) }
    if (history === 'new') { this.undoText.push(snapshot(before.text)); this.redoText = [] }
    if (history === 'undo') { this.undoText.pop(); this.redoText.push(snapshot(before.text)) }
    if (history === 'redo') { this.redoText.pop(); this.undoText.push(snapshot(before.text)) }
    this.trimHistory()
    this.publish({ ...this.state, document: updated!, draftText: preserveDraft ? this.state.draftText : text,
      inspection: null, preview: null, error: null })
    try {
      const inspection = await this.api.inspectDocument(updated!.document_id, updated!.revision)
      if (inspection.revision !== updated!.revision || inspection.validity !== updated!.validity) {
        throw codedError('unavailable', 'Graph inspection does not match the updated revision')
      }
      this.publish({ ...this.state, inspection,
        lastValid: inspection.validity === 'valid' ? inspection : this.state.lastValid, error: null })
    } catch (cause) { this.fail(cause) }
  }

  updateText(text: string): Promise<void> {
    this.publish({ ...this.state, draftText: text, pendingUpdates: this.state.pendingUpdates + 1,
      preview: null, error: null })
    return this.queue(async () => {
      try { await this.applyText(text, 'new', true) }
      finally { this.publish({ ...this.state, pendingUpdates: this.state.pendingUpdates - 1 }) }
    })
  }

  undo(): Promise<void> {
    return this.queue(async () => {
      if (this.state.externalConflict || (this.state.pendingUpdates === 0 && this.state.draftText !== this.state.document.text)) {
        throw codedError('conflict', 'Resolve the unsent graph draft before using Undo')
      }
      const previous = this.undoText.at(-1)
      if (previous !== undefined) await this.applyText(previous.text, 'undo', this.state.pendingUpdates > 0)
    })
  }

  redo(): Promise<void> {
    return this.queue(async () => {
      if (this.state.externalConflict || (this.state.pendingUpdates === 0 && this.state.draftText !== this.state.document.text)) {
        throw codedError('conflict', 'Resolve the unsent graph draft before using Redo')
      }
      const next = this.redoText.at(-1)
      if (next !== undefined) await this.applyText(next.text, 'redo', this.state.pendingUpdates > 0)
    })
  }

  previewVisual(operation: GraphEditOperation): Promise<GraphPreview> {
    return this.queue(async () => {
      if (!this.state.canVisualEdit) throw codedError('invalid_request', 'Correct the graph draft before visual editing')
      const { document } = this.state
      let preview: GraphPreview
      try {
        preview = await this.api.previewDocumentEdit(document.document_id, document.revision, operation)
        if (preview.base_revision !== document.revision || preview.validity !== 'valid') {
          throw codedError('unavailable', 'Graph preview does not match the selected revision')
        }
      } catch (cause) { this.fail(cause, false) }
      this.publish({ ...this.state, preview: preview!, error: null })
      return preview!
    })
  }

  acceptPreview(acceptReformat: boolean): Promise<void> {
    return this.queue(async () => {
      const preview = this.state.preview
      if (!preview || preview.base_revision !== this.state.document.revision) {
        throw codedError('conflict', 'Graph preview is no longer current')
      }
      if (preview.reformat_required && !acceptReformat) {
        throw codedError('reformat_required', 'Review the full YAML reformat before applying the visual edit')
      }
      await this.applyText(preview.text, 'new')
    })
  }

  discardPreview(): void {
    if (this.state.preview !== null) this.publish({ ...this.state, preview: null })
  }

  save(): Promise<void> {
    return this.queue(async () => {
      const before = this.state.document
      if (this.state.draftText !== before.text || this.state.pendingUpdates > 0) {
        throw codedError('conflict', 'Graph draft has not been accepted by the local service')
      }
      let saved: DocumentView
      try {
        saved = await this.api.saveDocument(before.document_id, before.revision, this.savedDigest, false)
        if (saved.document_id !== before.document_id || saved.revision !== before.revision || saved.text !== before.text) {
          throw codedError('unavailable', 'Graph save response does not match the submitted revision')
        }
      } catch (cause) { this.fail(cause) }
      this.savedText = saved!.text
      this.savedDigest = saved!.disk_digest
      this.publish({ ...this.state, document: saved!, draftText: saved!.text,
        preview: null, externalConflict: false, error: null })
    })
  }

  reloadDisk(discardDraft: boolean): Promise<void> {
    return this.queue(async () => {
      if (this.state.dirty && !discardDraft) {
        throw codedError('conflict', 'Reload would discard an unsaved graph draft')
      }
      const before = this.state.document
      let reloaded: DocumentView
      try {
        reloaded = await this.api.reloadDocument(before.document_id, before.revision, discardDraft)
        if (reloaded.document_id !== before.document_id || reloaded.revision <= before.revision) {
          throw codedError('unavailable', 'Graph reload response does not advance the selected document')
        }
      } catch (cause) { this.fail(cause) }
      this.savedText = reloaded!.text
      this.savedDigest = reloaded!.disk_digest
      this.undoText = []
      this.redoText = []
      this.publish({ ...this.state, document: reloaded!, draftText: reloaded!.text, inspection: null, preview: null,
        lastValid: null, externalConflict: false, error: null })
      try {
        const inspection = await this.api.inspectDocument(reloaded!.document_id, reloaded!.revision)
        if (inspection.revision !== reloaded!.revision || inspection.validity !== reloaded!.validity) {
          throw codedError('unavailable', 'Graph inspection does not match the reloaded revision')
        }
        this.publish({ ...this.state, inspection, lastValid: inspection.validity === 'valid' ? inspection : null })
      } catch (cause) { this.fail(cause) }
    })
  }

  keepDraft(): void {
    if (this.state.error !== null) this.publish({ ...this.state, error: null })
  }
}
