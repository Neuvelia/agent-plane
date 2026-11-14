import { mkdtempSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { MAX, createRecents } from '../electron/recents.cjs'

function recents() {
  const dir = mkdtempSync(join(tmpdir(), 'ap-recents-'))
  const file = join(dir, 'sub', 'recent.json')
  let n = 0
  return { file, r: createRecents(file, { randomId: () => `id${++n}`, now: () => 1000 + n }) }
}

describe('recent graphs', () => {
  it('lists newest first with labels only, never paths', () => {
    const { r, file } = recents()
    r.add('/work/one.yaml')
    r.add('/work/two.yaml')
    expect(r.list()).toEqual([
      { id: 'id2', label: 'two.yaml' },
      { id: 'id1', label: 'one.yaml' },
    ])
    expect(JSON.stringify(r.list())).not.toContain('/work')
    expect(r.pathOf('id1')).toBe('/work/one.yaml')
    expect(JSON.parse(readFileSync(file, 'utf8')).version).toBe(1)
  })

  it('moves a reopened graph to the top without duplicating it', () => {
    const { r } = recents()
    r.add('/a/x.yaml')
    r.add('/a/y.yaml')
    r.add('/a/x.yaml')
    expect(r.list().map((i) => i.label)).toEqual(['x.yaml', 'y.yaml'])
  })

  it('keeps at most MAX entries', () => {
    const { r } = recents()
    for (let i = 0; i < MAX + 5; i++) r.add(`/a/g${i}.yaml`)
    expect(r.list()).toHaveLength(MAX)
    expect(r.list()[0]?.label).toBe(`g${MAX + 4}.yaml`)
  })

  it('removes an entry', () => {
    const { r } = recents()
    r.add('/a/x.yaml')
    const id = r.list()[0]!.id
    r.remove(id)
    expect(r.list()).toEqual([])
    expect(r.pathOf(id)).toBeUndefined()
  })

  it('writes a private file', () => {
    const { r, file } = recents()
    r.add('/a/x.yaml')
    if (process.platform !== 'win32') expect(statSync(file).mode & 0o777).toBe(0o600)
  })

  it('starts empty after a corrupt or foreign file instead of failing', () => {
    const { r, file } = recents()
    r.add('/a/x.yaml')
    for (const junk of ['not json', '{"version":2,"items":[]}', '{"version":1,"items":[{"id":1}]}', 'null']) {
      writeFileSync(file, junk)
      expect(r.list()).toEqual([])
    }
    r.add('/a/y.yaml') // and it recovers
    expect(r.list().map((i) => i.label)).toEqual(['y.yaml'])
  })

  it('never throws when the history cannot be written', () => {
    // The parent "directory" is a regular file, so creating the history fails.
    const blocker = join(mkdtempSync(join(tmpdir(), 'ap-recents-')), 'blocker')
    writeFileSync(blocker, 'x')
    const r = createRecents(join(blocker, 'sub', 'recent.json'))
    expect(() => r.add('/a/x.yaml')).not.toThrow()
    expect(r.list()).toEqual([])
  })
})
