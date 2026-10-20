import type { DocumentView } from './api'

export function encodeText(text: string): string {
  const bytes = new TextEncoder().encode(text)
  let binary = ''
  for (let offset = 0; offset < bytes.length; offset += 8192) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
  }
  return btoa(binary)
}

export function documentView(raw: unknown): DocumentView {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) throw new Error('Invalid document response')
  const dto = raw as Record<string, unknown>
  if (typeof dto.text_base64 !== 'string' || typeof dto.document_id !== 'string' ||
      typeof dto.revision !== 'number' || !Number.isSafeInteger(dto.revision)) {
    throw new Error('Invalid document response')
  }
  const binary = atob(dto.text_base64)
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  const { text_base64: _encoded, ...rest } = dto
  return { ...rest, text } as DocumentView
}
