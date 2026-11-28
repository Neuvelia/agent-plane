import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { ICON_FILES, windowIcon } from '../electron/appIcon.cjs'

const PNG_MAGIC = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
const read = (p: string) => readFileSync(p)
// IHDR follows the 8-byte signature: length(4) 'IHDR'(4) width(4) height(4).
const pngSize = (b: Buffer) => {
  expect(b.subarray(0, 8).equals(PNG_MAGIC)).toBe(true)
  expect(b.subarray(12, 16).toString('latin1')).toBe('IHDR')
  return { w: b.readUInt32BE(16), h: b.readUInt32BE(20) }
}

// F2.5a: every file the window / Dock code points to exists and has the right format.
describe('app icon files', () => {
  it('points the window icon at the right file per platform', () => {
    expect(windowIcon('win32')).toBe(ICON_FILES.ico)
    expect(windowIcon('linux')).toBe(ICON_FILES.linux)
    expect(windowIcon('darwin')).toBeUndefined()
  })

  it('macOS Dock icon is a 1024x1024 PNG', () => {
    expect(pngSize(read(ICON_FILES.png))).toEqual({ w: 1024, h: 1024 })
  })

  it('Linux window icon is a 512x512 PNG', () => {
    expect(pngSize(read(ICON_FILES.linux))).toEqual({ w: 512, h: 512 })
  })

  it('every Linux size PNG exists with matching dimensions', () => {
    for (const n of [16, 24, 32, 48, 64, 128, 256, 512, 1024]) {
      const p = ICON_FILES.linux.replace('512x512', `${n}x${n}`)
      expect(pngSize(read(p)), `${n}x${n}.png`).toEqual({ w: n, h: n })
    }
  })

  it('icon.ico is an ICO with PNG-encoded 16-256 px images', () => {
    const b = read(ICON_FILES.ico)
    expect(b.readUInt16LE(0)).toBe(0) // reserved
    expect(b.readUInt16LE(2)).toBe(1) // type: icon
    const count = b.readUInt16LE(4)
    expect(count).toBe(7)
    const sizes: number[] = []
    for (let i = 0; i < count; i++) {
      const e = 6 + i * 16
      sizes.push(b.readUInt8(e) || 256) // width byte 0 means 256
      const offset = b.readUInt32LE(e + 12)
      expect(b.subarray(offset, offset + 8).equals(PNG_MAGIC)).toBe(true)
    }
    expect(sizes.sort((a, c) => a - c)).toEqual([16, 24, 32, 48, 64, 128, 256])
  })

  it('icon.icns has the icns magic and a matching length', () => {
    const b = read(ICON_FILES.icns)
    expect(b.subarray(0, 4).toString('latin1')).toBe('icns')
    expect(b.readUInt32BE(4)).toBe(b.length)
  })
})
