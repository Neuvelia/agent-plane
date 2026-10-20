import { describe, expect, it } from 'vitest'
import { documentView, encodeText } from './document'

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
})
