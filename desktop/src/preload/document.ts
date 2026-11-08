import type { DocumentView } from './api'
import type { GraphPreview } from '../renderer/graphDocument'

const maxDocumentBytes = 4 << 20

function onlyFields(value: Record<string, unknown>, allowed: readonly string[]): void {
  if (Object.keys(value).some((field) => !allowed.includes(field))) throw new Error('Undeclared document response field')
}

function diagnosticFields(raw: unknown): void {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) throw new Error('Invalid document diagnostics')
  const summary = raw as Record<string, unknown>
  onlyFields(summary, ['diagnostics', 'diagnostics_total', 'diagnostics_omitted', 'has_errors'])
  if (!Array.isArray(summary.diagnostics)) throw new Error('Invalid document diagnostics')
  for (const rawDiagnostic of summary.diagnostics) {
    if (typeof rawDiagnostic !== 'object' || rawDiagnostic === null || Array.isArray(rawDiagnostic)) throw new Error('Invalid document diagnostics')
    onlyFields(rawDiagnostic as Record<string, unknown>, ['code', 'severity', 'message', 'graph_id', 'node_id',
      'edge_index', 'graph_input_binding_index', 'path', 'line', 'column', 'suggestion'])
  }
}

export function encodeText(text: string): string {
  const bytes = new TextEncoder().encode(text)
  let binary = ''
  for (let offset = 0; offset < bytes.length; offset += 8192) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
  }
  return btoa(binary)
}

function decodeText(raw: unknown): string {
  if (typeof raw !== 'string' || raw.length > 4 * Math.ceil(maxDocumentBytes / 3)) {
    throw new Error('Invalid document encoding')
  }
  const binary = atob(raw)
  if (binary.length > maxDocumentBytes || btoa(binary) !== raw) throw new Error('Invalid document encoding')
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}

export function documentView(raw: unknown): DocumentView {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) throw new Error('Invalid document response')
  const dto = raw as Record<string, unknown>
  onlyFields(dto, ['document_id', 'project_id', 'relative_path', 'text_base64', 'revision',
    'disk_digest', 'validity', 'semantic_version', 'diagnostics', 'diagnostics_total',
    'diagnostics_omitted', 'has_errors'])
  if (dto.diagnostics !== undefined) diagnosticFields({ diagnostics: dto.diagnostics,
    diagnostics_total: dto.diagnostics_total, diagnostics_omitted: dto.diagnostics_omitted,
    has_errors: dto.has_errors })
  if (typeof dto.text_base64 !== 'string' || typeof dto.document_id !== 'string' ||
      typeof dto.revision !== 'number' || !Number.isSafeInteger(dto.revision)) {
    throw new Error('Invalid document response')
  }
  const text = decodeText(dto.text_base64)
  const { text_base64: _encoded, ...rest } = dto
  return { ...rest, text } as DocumentView
}

export function previewView(raw: unknown): GraphPreview {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) throw new Error('Invalid graph preview')
  const dto = raw as Record<string, unknown>
  onlyFields(dto, ['base_revision', 'text_base64', 'validity', 'diagnostics', 'reformat_required', 'changed_paths'])
  diagnosticFields(dto.diagnostics)
  if (typeof dto.base_revision !== 'number' || !Number.isSafeInteger(dto.base_revision) || dto.base_revision < 1 ||
      (dto.validity !== 'valid' && dto.validity !== 'invalid') ||
      typeof dto.reformat_required !== 'boolean' || !Array.isArray(dto.changed_paths) ||
      dto.changed_paths.some((path) => typeof path !== 'string')) throw new Error('Invalid graph preview')
  const text = decodeText(dto.text_base64)
  const { text_base64: _encoded, ...rest } = dto
  return { ...rest, text } as GraphPreview
}
