import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import bindingsOpen from './fixtures/graph-open-bindings.json'
import mixedOpen from './fixtures/graph-open-mixed.json'
import { BindingsPanel } from '../src/editor/BindingsPanel'
import type { Selection } from '../src/editor/selection'
import { TargetOptions, canSend } from '../src/editor/TargetOptions'
import type { ConnectionOption, Definition } from '../src/editor/types'
import type { GraphDocument } from '../src/editor/useGraphDocument'
import { I18nProvider } from '../src/i18n/context'
import type { Diagnostic } from '../src/types'

const def = (o: unknown) => (o as { definition: Definition }).definition
const bindings = def(bindingsOpen)
const mixed = def(mixedOpen)
const diagnosticsOf = (o: unknown) => (o as { diagnostics: Diagnostic[] }).diagnostics

describe('a refused connection option', () => {
  const refused: ConnectionOption = { ref: 'b.inputs.count', type: 'number', required: false, ok: false, code: 'E_X', message: 'edge binds "c.outputs.stdout" (string) to "b.inputs.count" (number)' }
  const accepted: ConnectionOption = { ref: 'b.inputs.text', type: 'string', required: true, ok: true }
  const html = (locale: 'en' | 'pl') =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <TargetOptions options={[refused, accepted]} label="targets" onPick={() => undefined} />
      </I18nProvider>,
    )

  it('is never sendable', () => {
    expect(canSend(refused)).toBe(false)
    expect(canSend(accepted)).toBe(true)
  })

  it('stays in the list, aria-disabled, with the engine message as visible text the button is described by', () => {
    const out = html('en')
    const item = out.split('<li').find((x) => x.includes('b.inputs.count'))!
    expect(item).toContain('aria-disabled="true"')
    expect(item).toContain(refused.message!.replace(/"/g, '&quot;'))
    const describedBy = /aria-describedby="([^"]+)"/.exec(item)![1]
    expect(item).toContain(`id="${describedBy}"`)
    expect(item).toContain('✖')
    expect(out.split('<li').find((x) => x.includes('b.inputs.text'))).not.toContain('aria-disabled')
  })

  it('is the same in Polish, with the engine message untranslated', () => {
    const out = html('pl')
    expect(out).toContain('Połącz z b.inputs.count')
    expect(out).toContain(refused.message!.replace(/"/g, '&quot;'))
  })
})

// The panel as markup: rows, pressed state and labels. Clicks are covered by the stories and the e2e.
describe('the Bindings panel markup', () => {
  const g = {} as GraphDocument
  const html = (locale: 'en' | 'pl', selection: Selection, readOnly = false, d = bindings, diags: Diagnostic[] = []) =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <BindingsPanel g={g} def={d} diagnostics={diags} selection={{ selection, select: () => undefined }} readOnly={readOnly} />
      </I18nProvider>,
    )

  it('is a table with a caption and column headers, one row per binding', () => {
    const out = html('en', null)
    expect(out).toContain('<table')
    expect(out).toMatch(/<caption[^>]*>[^<]+<\/caption>/)
    expect(out.match(/scope="col"/g)).toHaveLength(4)
    expect(out.match(/data-testid="binding-row"/g)).toHaveLength(2)
  })

  it('makes the first cell of each row a button, pressed for the selected one', () => {
    const out = html('en', { kind: 'edge', from: 'a.outputs.stdout', to: 'b.inputs.text' })
    const rows = out.split('<tr').filter((r) => r.includes('binding-row'))
    expect(rows[0]).toMatch(/<th scope="row"><button[^>]*aria-pressed="true"/)
    expect(rows[1]).toMatch(/<th scope="row"><button[^>]*aria-pressed="false"/)
  })

  it('does not press a row for a node selection or for a selection of the other kind with the same refs', () => {
    expect(html('en', { kind: 'node', id: 'b' })).not.toContain('aria-pressed="true"')
    expect(html('en', { kind: 'binding', from: 'a.outputs.stdout', to: 'b.inputs.text' })).not.toContain('aria-pressed="true"')
  })

  it('says status in words and glyphs: ok, error and warning', () => {
    const ok = html('en', null)
    expect(ok).toContain('✓ ok')
    const bad = html('en', null, false, mixed, diagnosticsOf(mixedOpen))
    expect(bad).toContain('✖ 1 error')
  })

  it('has a Remove button per row named by its binding, disabled while read-only, which is explained', () => {
    const out = html('en', null)
    expect(out).toContain('aria-label="Remove the connection a.outputs.stdout to b.inputs.text"')
    const ro = html('en', null, true)
    expect(ro).toContain('data-testid="bindings-readonly"')
    expect(ro.match(/data-testid="remove-binding"/g)).toHaveLength(2)
    expect(ro.match(/disabled=""[^>]*data-testid="remove-binding"|data-testid="remove-binding"[^>]*disabled=""/g)?.length ?? 0).toBeGreaterThanOrEqual(2)
  })

  it('shows the empty state, and is localised', () => {
    const empty = html('en', null, false, { id: 'e', nodes: {}, edges: [], outputs: {} })
    expect(empty).toContain('data-testid="bindings-empty"')
    expect(empty).not.toContain('<table')
    expect(html('pl', null)).toContain('Powiązania')
  })

  it('announces politely through a status region', () => {
    expect(html('en', null)).toMatch(/role="status" aria-live="polite"[^>]*data-testid="bindings-live"/)
  })
})
