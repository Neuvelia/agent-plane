import { describe, expect, it } from 'vitest'
import { authorizedSender, externalHTTPS, safeUnsigned } from './security'

describe('Electron main security guards', () => {
  it('requires exact main frame and window sender', () => {
    const frame = { url: 'file:///app/index.html' }
    const contents = { mainFrame: frame, getURL: () => frame.url }
    const window = { isDestroyed: () => false, webContents: contents }
    expect(authorizedSender({ sender: contents, senderFrame: frame } as never, window as never)).toBe(true)
    expect(authorizedSender({ sender: contents, senderFrame: { url: frame.url } } as never, window as never)).toBe(false)
    expect(authorizedSender({ sender: {}, senderFrame: frame } as never, window as never)).toBe(false)
    expect(authorizedSender({ sender: contents, senderFrame: frame } as never, null)).toBe(false)
  })

  it('allows only explicit HTTPS links without embedded credentials', () => {
    expect(externalHTTPS('https://example.com/path')).toBe('https://example.com/path')
    for (const value of ['javascript:alert(1)', 'file:///etc/passwd', 'http://example.com',
      'https://user:password@example.com', 'https://', 7]) {
      expect(() => externalHTTPS(value)).toThrow()
    }
  })

  it('rejects numeric identities at the JavaScript precision boundary', () => {
    expect(safeUnsigned(0)).toBe(0)
    expect(safeUnsigned(Number.MAX_SAFE_INTEGER)).toBe(Number.MAX_SAFE_INTEGER)
    for (const value of [-1, 1.5, 2 ** 53, 2 ** 53 + 1, '1']) expect(() => safeUnsigned(value)).toThrow()
  })
})
