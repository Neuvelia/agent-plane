import { readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

// macOS and Windows file systems ignore case, so `./ValuePicker` and `./valuePicker`
// resolve to the same file there while Linux CI tells them apart. Imports name
// modules without their extension, so two modules must differ by more than case.
function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else yield p
  }
}

describe('module names', () => {
  it('no two source modules differ only by letter case', () => {
    const src = fileURLToPath(new URL('../src', import.meta.url))
    const seen = new Map<string, string>()
    const clashes: string[] = []
    for (const file of walk(src)) {
      if (!/\.(ts|tsx)$/.test(file)) continue
      const module = relative(src, file).replace(/\.(ts|tsx)$/, '')
      const key = module.toLowerCase()
      const other = seen.get(key)
      if (other !== undefined && other !== module) clashes.push(`${other} / ${module}`)
      seen.set(key, module)
    }
    expect(clashes).toEqual([])
  })
})
