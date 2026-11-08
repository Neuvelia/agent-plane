import { describe, expect, it } from 'vitest'
import { documentView, encodeText, previewView } from './document'

describe('document text bridge', () => {
  it('round-trips a 4 MiB UTF-8 buffer without changing authored text', () => {
    const text = `${'a'.repeat((4 << 20) - 4)}é\n`
    const view = documentView({ document_id: 'doc', revision: 1, text_base64: encodeText(text) })
    expect(view.text).toBe(text)
    expect(Object.hasOwn(view, 'text_base64')).toBe(false)
  })

  it('rejects an unsafe revision or invalid UTF-8 response', () => {
    expect(() => documentView({ document_id: 'doc', revision: 2 ** 53, text_base64: '' })).toThrow()
    expect(() => documentView({ document_id: 'doc', revision: 1, text_base64: '/w==' })).toThrow()
  })

  it('decodes an exact UTF-8 visual preview and refuses noncanonical base64', () => {
    const text = 'schema_version: 0\n# café\n'
    const raw = { base_revision: 3, text_base64: encodeText(text), validity: 'valid',
      diagnostics: { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false },
      reformat_required: true, changed_paths: ['nodes.plan'] }
    expect(previewView(raw)).toMatchObject({ base_revision: 3, text, reformat_required: true })
    expect(Object.hasOwn(previewView(raw), 'text_base64')).toBe(false)
    expect(() => previewView({ ...raw, text_base64: `${raw.text_base64}\n` })).toThrow()
    expect(() => previewView({ ...raw, text_base64: '/w==' })).toThrow()
    expect(() => previewView({ ...raw, base_revision: 2 ** 53 })).toThrow()
  })

  it('does not expose undeclared sidecar fields in document or preview objects', () => {
    const summary = { diagnostics: [], diagnostics_total: 0, diagnostics_omitted: 0, has_errors: false }
    const document = { document_id: 'doc', project_id: 'project', relative_path: 'graph.yaml',
      revision: 1, disk_digest: 'digest', validity: 'valid', text_base64: 'eA==', ...summary }
    expect(() => documentView({ ...document, config: { credential: 'private-token' } })).toThrow()
    const preview = { base_revision: 1, validity: 'valid', text_base64: 'eA==',
      diagnostics: summary, reformat_required: false, changed_paths: [] }
    expect(() => previewView({ ...preview, config: { credential: 'private-token' } })).toThrow()
  })
})
