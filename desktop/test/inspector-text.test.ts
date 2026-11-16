import { describe, expect, it } from 'vitest'
import nodeTypes from './fixtures/node-types.json'
import { fieldCatalogues, fieldText } from '../src/editor/inspector/fieldText'
import type { FieldDescriptor, NodeTypeDescriptor } from '../src/editor/types'

// F2.2: every descriptor id the sidecar sends has a label and help in EN and PL.
// node-types.json is pinned to the Go catalogue by cmd/agentplane/catalogue_test.go,
// so a descriptor added in Go without help text fails here.
function ids(fs: FieldDescriptor[] = []): string[] {
  return fs.flatMap((f) => [f.id, ...(f.item ? ids([f.item]) : []), ...ids(f.fields)])
}
const all = [...new Set(Object.values(nodeTypes as unknown as Record<string, NodeTypeDescriptor>).flatMap((t) => [...ids(t.config), ...ids(t.loop?.fields), ...ids(t.loop?.gate)]))].sort()

describe('field help catalogue', () => {
  it('covers every descriptor id in both languages', () => {
    expect(all.length).toBeGreaterThan(40)
    for (const locale of ['en', 'pl'] as const) {
      const missing = all.filter((id) => !fieldCatalogues[locale][id]?.label.trim() || !fieldCatalogues[locale][id]?.help.trim())
      expect(missing, locale).toEqual([])
    }
  })
  it('has no entries for ids the sidecar does not send', () => {
    expect(Object.keys(fieldCatalogues.en).sort()).toEqual(all)
    expect(Object.keys(fieldCatalogues.pl).sort()).toEqual(all)
  })
  it('falls back to English, then to the key', () => {
    expect(fieldText('pl', 'shell.config.command').label).toBe('Polecenie')
    expect(fieldText('pl', 'no.such.id', 'mystery')).toEqual({ label: 'mystery', help: '' })
  })
})
