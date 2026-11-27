import { readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import valueEvents from './fixtures/demo-approve-values.json'
import { I18nProvider } from '../src/i18n/context'
import type { ApEvent, RunSummary } from '../src/types'
import { ValuePicker } from '../src/ValuePicker'
import type { ValuesPicker } from '../src/useRunValues'
import { choose, confirmVersion, noChoice, pickerRuns, resolveRun } from '../src/valuePickerModel'
import { outputSlot, reduceAllValues } from '../src/values'

// The picker model and the values reducer are tested in edge-values.test.ts; this file is the view and privacy.
const stream = valueEvents as unknown as ApEvent[]
const RUN = stream[0]!.run_id

const run = (id: string, startedAt: string, version: string | undefined): RunSummary => ({ run_id: id, graph_id: 'demo', started_at: startedAt, status: 'succeeded', event_count: 10, graph_version: version })
const runs: RunSummary[] = [run('old-same', '2026-11-10T10:00:00Z', 'v2'), run('new-other', '2026-11-16T10:00:00Z', 'v1'), run('new-same', '2026-11-15T10:00:00Z', 'v2')]

describe('privacy', () => {
  const real = { local: Object.getOwnPropertyDescriptor(globalThis, 'localStorage'), session: Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage') }
  afterEach(() => {
    vi.restoreAllMocks()
    for (const [name, d] of [['localStorage', real.local], ['sessionStorage', real.session]] as const) {
      if (d) Object.defineProperty(globalThis, name, d)
      else delete (globalThis as Record<string, unknown>)[name]
    }
  })

  it('writes to no storage and calls no console method with a value', () => {
    const writes: string[] = []
    const fake = { getItem: () => null, setItem: (k: string, v: string) => writes.push(`${k}=${v}`), removeItem: () => undefined, clear: () => undefined, key: () => null, length: 0 }
    Object.defineProperty(globalThis, 'localStorage', { value: fake, configurable: true })
    Object.defineProperty(globalThis, 'sessionStorage', { value: fake, configurable: true })
    const spies = (['log', 'info', 'warn', 'error', 'debug', 'trace'] as const).map((m) => vi.spyOn(console, m).mockImplementation(() => undefined))
    const v = reduceAllValues(RUN, stream)
    const fast = outputSlot(v, 'fast', 'stdout')
    expect(fast?.kind === 'value' && fast.value.value).toBe('fast branch done')
    renderToStaticMarkup(
      <I18nProvider locale="en">
        <ValuePicker picker={picker({ stream: { load: 'ready', values: v, lost: false, reconnect: () => undefined } })} />
      </I18nProvider>,
    )
    expect(writes).toEqual([])
    for (const spy of spies) expect(spy).not.toHaveBeenCalled()
  })

  it('the values modules contain no storage, console or network use', () => {
    for (const file of ['values.ts', 'valuePickerModel.ts', 'useRunValues.ts', 'ValuePicker.tsx']) {
      const source = readFileSync(new URL(`../src/${file}`, import.meta.url), 'utf8')
      expect(source, file).not.toMatch(/localStorage|sessionStorage|indexedDB|console\.|fetch\(|XMLHttpRequest/)
    }
  })
})

describe('ValuePicker view', () => {
  const html = (p: ValuesPicker, locale: 'en' | 'pl' = 'en') =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <ValuePicker picker={p} />
      </I18nProvider>,
    )

  it('says there is no run yet, never an empty select', () => {
    const out = html(picker({ runs: [] }))
    expect(out).toContain('data-testid="value-picker-none"')
    expect(out).not.toContain('<select')
  })

  it('asks for confirmation before a different-version run and tags it', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    const out = html(picker({ runs: list, choice: choose(noChoice, 'new-other') }))
    expect(out).toContain('data-testid="value-picker-confirm"')
    expect(out).toContain('different version')
    expect(out).not.toContain('value-picker-status">1')
  })

  it('shows a confirmed different-version run with its label, and the same-version default without', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    const confirmed = html(picker({ runs: list, choice: confirmVersion(choose(noChoice, 'new-other'), 'new-other') }))
    expect(confirmed).toContain('data-testid="value-picker-different"')
    expect(confirmed).not.toContain('data-testid="value-picker-confirm"')
    const normal = html(picker({ runs: list }))
    expect(normal).not.toContain('data-testid="value-picker-different"')
    expect(normal).not.toContain('data-testid="value-picker-confirm"')
  })

  it('reports an unreadable run, a lost connection and loading in both languages', () => {
    const list = pickerRuns(runs, 'demo', 'v2')
    expect(html(picker({ runs: list, stream: { load: 'unavailable', lost: false, reconnect: () => undefined } }))).toContain('This run could not be read.')
    expect(html(picker({ runs: list, stream: { load: 'unavailable', lost: false, reconnect: () => undefined } }), 'pl')).toContain('Nie udało się odczytać tego uruchomienia.')
    expect(html(picker({ runs: list, stream: { load: 'ready', values: reduceAllValues(RUN, stream), lost: true, reconnect: () => undefined } }))).toContain('data-testid="value-picker-lost"')
    expect(html(picker({ runs: list, stream: { load: 'loading', lost: false, reconnect: () => undefined } }))).toContain('Loading values')
    expect(html(picker({ runs: list, stream: { load: 'ready', values: reduceAllValues(RUN, stream), lost: false, reconnect: () => undefined } }))).toContain('3 values recorded')
  })

  it('no run of this version: says so and offers the others', () => {
    const out = html(picker({ runs: pickerRuns(runs, 'demo', 'v9') }))
    expect(out).toContain('data-testid="value-picker-no-match"')
    expect(out).toContain('Choose a run')
  })
})

function picker(over: Partial<ValuesPicker> & { choice?: ValuesPicker['choice'] }): ValuesPicker {
  const list = over.runs ?? pickerRuns(runs, 'demo', 'v2')
  const choice = over.choice ?? noChoice
  return { runs: list, runsStatus: 'ready', choice, resolved: resolveRun(list, choice), stream: { load: 'idle', lost: false, reconnect: () => undefined }, choose: () => undefined, confirm: () => undefined, ...over }
}
