import { readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import demoOpen from './fixtures/graph-open-demo.json'
import bindingsOpen from './fixtures/graph-open-bindings.json'
import valueEvents from './fixtures/demo-approve-values.json'
import demoApprove from './fixtures/demo-approve.json'
import demoReject from './fixtures/demo-reject.json'
import { EDGE_VALUE_STATES, edgeValueView, type EdgeValueInput, type EdgeValueState } from '../src/editor/edgeValue'
import { EdgeInspector } from '../src/editor/EdgeInspector'
import { copyDisplayed } from '../src/editor/EdgeValueBox'
import type { Definition } from '../src/editor/types'
import { carriedBytes, displayText, formatJson, visibleParts } from '../src/editor/valueText'
import { I18nProvider } from '../src/i18n/context'
import type { ApEvent, RunSummary } from '../src/types'
import type { RunValuesStream, ValuesPicker } from '../src/useRunValues'
import { noChoice, pickerRuns, resolveRun, choose, type RunChoice } from '../src/valuePicker'
import { reduceAllValues } from '../src/values'

// The edge inspector's states (F2.3 requirement 17), text safety and view. Streams are the
// recorded demo runs (demo-approve-values.json is schema 10 with recording on; demo-approve.json and
// demo-reject.json are schema 9); a variant removes or changes one value event, as a real run
// would have recorded it with another input.
const recorded = valueEvents as unknown as ApEvent[]
const RUN = recorded[0]!.run_id
const VERSION = recorded[0]!.graph_version!
const demo = (demoOpen as unknown as { definition: Definition }).definition

const isValue = (port: string) => (e: ApEvent) => e.type === 'value_recorded' && e.payload.port === port
const without = (events: ApEvent[], drop: (e: ApEvent) => boolean) => events.filter((e) => !drop(e))
const patched = (events: ApEvent[], port: string, payload: Record<string, unknown>): ApEvent[] => events.map((e) => (isValue(port)(e) ? { ...e, payload: { ...e.payload, ...payload } } : e))
const until = (events: ApEvent[], pred: (e: ApEvent) => boolean) => events.slice(0, events.findIndex(pred) + 1)

const summary = (version: string | undefined): RunSummary => ({ run_id: RUN, graph_id: 'demo-vertical', started_at: '2026-11-15T22:04:46Z', status: 'succeeded', event_count: 31, graph_version: version })
const reconnect = () => undefined

interface Setup {
  runs?: RunSummary[]
  choice?: RunChoice
  runsStatus?: 'loading' | 'ready' | 'error'
  stream?: Partial<RunValuesStream>
  events?: ApEvent[]
}

function picker(s: Setup = {}): ValuesPicker {
  const list = pickerRuns(s.runs ?? [summary(VERSION)], 'demo-vertical', VERSION)
  const choice = s.choice ?? noChoice
  const resolved = resolveRun(list, choice)
  const events = s.events ?? recorded
  return { runs: list, runsStatus: s.runsStatus ?? 'ready', choice, resolved, stream: { load: 'ready', values: reduceAllValues(events[0]?.run_id ?? RUN, events), lost: false, reconnect, ...s.stream }, choose: () => undefined, confirm: () => undefined }
}

const viewOf = (from: string, s: Setup = {}) => {
  const p = picker(s)
  const input: EdgeValueInput = { runs: p.runs, runsStatus: p.runsStatus, resolved: p.resolved, stream: p.stream, from }
  return edgeValueView(input)
}

const FAST = 'fast.outputs.stdout'
const EVIDENCE = 'flaky.outputs.json'

describe('edge value states and their precedence', () => {
  it('the recorded runs the stories use are of the version of the graph they open', () => {
    expect((demoOpen as unknown as { version: string }).version).toBe(VERSION)
  })

  it('a recorded value is the value state, with its provenance', () => {
    const v = viewOf(FAST)
    expect(v).toMatchObject({ state: 'value', value: { value: 'fast branch done', type: 'string' } })
  })

  it('no run: the graph has not run; loading and a failed list are not "no run"', () => {
    expect(viewOf(FAST, { runs: [] }).state).toBe('no_run')
    expect(viewOf(FAST, { runs: [], runsStatus: 'loading' }).state).toBe('loading')
    expect(viewOf(FAST, { runs: [], runsStatus: 'error' }).state).toBe('run_unavailable')
  })

  it('run unavailable: the chosen run could not be read, and it beats everything after it', () => {
    expect(viewOf(FAST, { stream: { load: 'unavailable', values: undefined } }).state).toBe('run_unavailable')
    expect(viewOf(FAST, { stream: { load: 'ready', values: reduceAllValues(RUN, []) } }).state).toBe('run_unavailable')
  })

  it('different version: withheld until confirmed, whether or not a run is chosen', () => {
    const other = [summary('sha256-v0:other')]
    expect(viewOf(FAST, { runs: other })).toEqual({ state: 'different_version', chosen: false })
    expect(viewOf(FAST, { runs: other, choice: choose(noChoice, RUN) })).toEqual({ state: 'different_version', chosen: true })
    expect(viewOf(FAST, { runs: [summary(undefined)], choice: choose(noChoice, RUN) }).state).toBe('different_version')
    // Confirmed: the value is shown (the picker labels it as another version).
    const confirmed = { choice: { runId: RUN, confirmed: [RUN] }, runs: other }
    expect(viewOf(FAST, confirmed).state).toBe('value')
  })

  it('node not finished: pending, running and waiting for a person', () => {
    expect(viewOf(FAST, { events: until(recorded, (e) => e.type === 'run_started') })).toEqual({ state: 'node_not_finished', status: 'pending' })
    expect(viewOf(FAST, { events: until(recorded, (e) => e.type === 'node_started' && e.node_id === 'fast') })).toEqual({ state: 'node_not_finished', status: 'running' })
    expect(viewOf('approve.outputs.decision', { events: until(recorded, (e) => e.type === 'approval_requested') })).toEqual({ state: 'node_not_finished', status: 'waiting_human' })
  })

  it('node failed or skipped has no accepted value, and beats "older run"', () => {
    const rejected = demoReject as unknown as ApEvent[]
    expect(viewOf('approve.outputs.decision', { events: rejected })).toEqual({ state: 'node_failed_or_skipped', status: 'failed' })
    // The same schema-9 run, an edge whose source succeeded: nothing could have been recorded.
    expect(viewOf(FAST, { events: rejected }).state).toBe('older_run')
  })

  it('older run: schema 1-9 cannot carry values', () => {
    expect(viewOf(FAST, { events: demoApprove as unknown as ApEvent[] })).toEqual({ state: 'older_run', schemaVersion: 9 })
  })

  it('not recorded: schema 10 or later with no value event at all', () => {
    expect(viewOf(FAST, { events: without(recorded, (e) => e.type === 'value_recorded') }).state).toBe('not_recorded')
  })

  it('no value for this port: values exist, none for this key', () => {
    expect(viewOf(EVIDENCE, { events: without(recorded, isValue('json')) }).state).toBe('no_value_for_port')
    // A graph input that was never supplied is the same state, not "not recorded".
    expect(viewOf('inputs.topic').state).toBe('no_value_for_port')
  })

  it('value not carried: the event exists without text and keeps its size, hash and marks', () => {
    const events = patched(recorded, 'stdout', { value: undefined, truncated: true, size_bytes: 20000, redacted: true })
    const v = viewOf(FAST, { events })
    expect(v).toMatchObject({ state: 'value_not_carried', value: { sizeBytes: 20000, truncated: true, redacted: true } })
  })

  it('an empty string and a JSON null are values, never "not recorded"', () => {
    expect(viewOf(FAST, { events: patched(recorded, 'stdout', { value: '', size_bytes: 0 }) })).toMatchObject({ state: 'value', value: { value: '' } })
    expect(viewOf(EVIDENCE, { events: patched(recorded, 'json', { value: 'null', size_bytes: 4 }) })).toMatchObject({ state: 'value', value: { value: 'null' } })
  })

  it('two node runs for one key are ambiguous and show neither value', () => {
    const second = { ...recorded.find(isValue('stdout'))!, sequence: 99, node_run_id: 'other', payload: { ...recorded.find(isValue('stdout'))!.payload, node_run_id: 'other', value: 'second' } }
    const v = viewOf(FAST, { events: [...recorded, second] })
    expect(v).toEqual({ state: 'ambiguous', sequences: [10, 99] })
  })

  it('every state of requirement 17 has its own heading in both languages', () => {
    for (const locale of ['en', 'pl'] as const) {
      const headings = EDGE_VALUE_STATES.map((state) => headingOf(state, locale))
      expect(new Set(headings).size, locale).toBe(EDGE_VALUE_STATES.length)
      for (const h of headings) expect(h, locale).not.toBe('')
    }
  })
})

// A setup that reaches each state, to render it.
const REACH: Record<EdgeValueState, [string, Setup]> = {
  loading: [FAST, { runs: [], runsStatus: 'loading' }],
  no_run: [FAST, { runs: [] }],
  run_unavailable: [FAST, { stream: { load: 'unavailable', values: undefined } }],
  different_version: [FAST, { runs: [summary('sha256-v0:other')] }],
  node_not_finished: [FAST, { events: until(recorded, (e) => e.type === 'run_started') }],
  node_failed_or_skipped: ['approve.outputs.decision', { events: demoReject as unknown as ApEvent[] }],
  older_run: [FAST, { events: demoApprove as unknown as ApEvent[] }],
  not_recorded: [FAST, { events: without(recorded, (e) => e.type === 'value_recorded') }],
  no_value_for_port: [EVIDENCE, { events: without(recorded, isValue('json')) }],
  ambiguous: [FAST, { events: [...recorded, { ...recorded.find(isValue('stdout'))!, sequence: 99, payload: { ...recorded.find(isValue('stdout'))!.payload, node_run_id: 'other' } }] }],
  value_not_carried: [FAST, { events: patched(recorded, 'stdout', { value: undefined, truncated: true }) }],
  value: [FAST, {}],
}

function html(from: string, s: Setup = {}, locale: 'en' | 'pl' = 'en', to = 'approve.inputs.summary', def: Definition = demo) {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <EdgeInspector def={def} edge={{ kind: from.startsWith('inputs.') ? 'binding' : 'edge', from, to }} picker={picker(s)} />
    </I18nProvider>,
  )
}

function headingOf(state: EdgeValueState, locale: 'en' | 'pl'): string {
  const [from, setup] = REACH[state]
  const out = html(from, setup, locale)
  const m = /data-testid="edge-value-heading">([^<]*)/.exec(out) ?? /data-testid="edge-value-state"[^>]*>([^<]*)/.exec(out)
  return m?.[1] ?? ''
}

describe('edge inspector view', () => {
  it('shows source, target and declared type for an edge and for a graph input binding', () => {
    const out = html(FAST)
    expect(out).toContain('data-testid="edge-source">fast.outputs.stdout')
    expect(out).toContain('data-testid="edge-target">approve.inputs.summary')
    expect(out).toContain('data-testid="edge-type">string')
    const inputs = (bindingsOpen as unknown as { definition: Definition }).definition
    const binding = html('inputs.topic', {}, 'en', 'c.inputs.seed', inputs)
    expect(binding).toContain('data-testid="edge-source">inputs.topic')
    expect(binding).toContain('data-testid="edge-type">string')
  })

  it('every reachable state renders in the state attribute it was computed as', () => {
    for (const [state, [from, setup]] of Object.entries(REACH)) expect(html(from, setup), state).toContain(`data-state="${state}"`)
  })

  it('a value sits in a labelled, focusable region and offers Copy; states without a value offer neither', () => {
    const out = html(FAST)
    expect(out).toMatch(/<div role="region" tabindex="0" aria-label="Value of fast.outputs.stdout" class="ev-region"/)
    expect(out).toContain('data-testid="edge-value-copy"')
    expect(out).toContain('aria-live="polite"')
    for (const state of Object.keys(REACH).filter((s) => s !== 'value')) {
      const [from, setup] = REACH[state as EdgeValueState]
      const other = html(from, setup)
      expect(other, state).not.toContain('data-testid="edge-value-copy"')
      expect(other, state).not.toContain('edge-value-region')
    }
  })

  it('each state message is a status line, and the state is a heading', () => {
    expect(html(FAST, REACH.not_recorded[1])).toMatch(/<h3[^>]*data-testid="edge-value-heading">Not recorded/)
    expect(html(FAST, REACH.not_recorded[1])).toMatch(/<p role="status"[^>]*data-testid="edge-value-message">/)
  })

  it('says "first N of M bytes" for a truncated value, and marks redacted ones', () => {
    const events = patched(recorded, 'stdout', { truncated: true, size_bytes: 20000, redacted: true })
    const out = html(FAST, { events })
    expect(out).toContain('first 16 of 20000 bytes')
    expect(out).toContain('truncated')
    expect(out).toContain('redacted')
  })

  it('a value not carried shows size, hash and marks and no text', () => {
    const out = html(FAST, REACH.value_not_carried[1])
    expect(out).toContain('data-testid="edge-value-facts"')
    expect(out).not.toContain('fast branch done')
    expect(out).toContain('6d4dc8f2acbfcdde81657a9ec76f5b116ddb25526bd230d8ae3f4086e64c760a')
  })

  it('an empty value and JSON null say so, and have no Copy', () => {
    const empty = html(FAST, { events: patched(recorded, 'stdout', { value: '', size_bytes: 0 }) })
    expect(empty).toContain('data-testid="edge-value-empty"')
    expect(empty).not.toContain('data-testid="edge-value-copy"')
    const nul = html(EVIDENCE, { events: patched(recorded, 'json', { value: 'null', size_bytes: 4 }) }, 'en', 'approve.inputs.evidence')
    expect(nul).toContain('data-testid="edge-value-empty"')
    expect(nul).toContain('state="value"')
    expect(html(FAST, REACH.not_recorded[1])).not.toContain('edge-value-empty')
  })

  it('shows hidden and bidirectional characters as visible marks and never as themselves', () => {
    const hostile = 'a\u202Eb\u0007c\u2066d\u200Fe\u009Ff\u0000g\tkeep\nline'
    const out = html(FAST, { events: patched(recorded, 'stdout', { value: hostile }) })
    for (const raw of ['\u202E', '\u0007', '\u2066', '\u200F', '\u009F', '\u0000']) expect(out).not.toContain(raw)
    for (const mark of ['\\u202E', '\\u0007', '\\u2066', '\\u200F', '\\u009F', '\\u0000']) expect(out).toContain(mark)
    expect(out).toContain('\t')
    expect(out).toContain('\n')
  })

  it('renders markup in a value as text', () => {
    const out = html(FAST, { events: patched(recorded, 'stdout', { value: '<img src=x onerror=alert(1)><a href="http://x">y</a>' }) })
    expect(out).not.toContain('<img')
    expect(out).not.toMatch(/<a\s/)
    expect(out).toContain('&lt;img src=x')
  })

  it('shows an artifact_ref as a plain string with no link or open action', () => {
    const out = html(FAST, { events: patched(recorded, 'stdout', { type: 'artifact_ref', value: 'file:///etc/passwd' }) })
    expect(out).toContain('file:///etc/passwd')
    expect(out).not.toMatch(/<a\s|href=|<button[^>]*open/i)
    expect(out).toContain('never opened')
  })

  it('a json value opens formatted with a raw toggle; text that is not JSON has no toggle', () => {
    const json = html(EVIDENCE, { events: patched(recorded, 'json', { value: '{"a":1,"b":[true,null]}' }) })
    expect(json).toContain('data-testid="edge-value-raw"')
    expect(json).toMatch(/data-testid="edge-value-formatted"[^>]*>Formatted/)
    expect(json).toContain('aria-pressed="true"')
    expect(json).toContain('&quot;a&quot;: 1')
    const broken = html(EVIDENCE, { events: patched(recorded, 'json', { value: '{"a":' }) })
    expect(broken).not.toContain('edge-value-raw')
    expect(broken).toContain('{&quot;a&quot;:')
  })

  it('says a lost connection may have made it incomplete, for values and for other states', () => {
    expect(html(FAST, { stream: { lost: true } })).toContain('edge-value-incomplete')
    expect(html(FAST, { events: until(recorded, (e) => e.type === 'run_started'), stream: { lost: true } })).toContain('edge-value-incomplete')
    expect(html(FAST)).not.toContain('edge-value-incomplete')
  })

  it('labels a value of another version', () => {
    const out = html(FAST, { runs: [summary('sha256-v0:other')], choice: { runId: RUN, confirmed: [RUN] } })
    expect(out).toContain('different version')
    expect(out).toContain('state="value"')
  })

  it('is localised', () => {
    const out = html(FAST, {}, 'pl')
    expect(out).toContain('Wartość')
    expect(out).toContain('Kopiuj')
    expect(out).toContain('aria-label="Wartość fast.outputs.stdout"')
  })
})

describe('displaying text safely', () => {
  it('marks C0, DEL, C1, separators and every bidirectional control, and keeps newline and tab', () => {
    const parts = visibleParts('a\n\tb\u0001\u007F\u0085\u2028\u202A\u202B\u202C\u202D\u202E\u2066\u2067\u2068\u2069\u200E\u200F\u061Cz')
    const marks = parts.filter((p) => p.mark).map((p) => p.text)
    expect(marks).toEqual(['\\u0001', '\\u007F', '\\u0085', '\\u2028', '\\u202A', '\\u202B', '\\u202C', '\\u202D', '\\u202E', '\\u2066', '\\u2067', '\\u2068', '\\u2069', '\\u200E', '\\u200F', '\\u061C'])
    expect(parts.filter((p) => !p.mark).map((p) => p.text).join('')).toBe('a\n\tbz')
  })

  it('leaves ordinary text, including other scripts and emoji, untouched', () => {
    const text = 'zażółć gęślą jaźń ✓ 日本語 😀 \\u0041'
    expect(displayText(text)).toBe(text)
  })

  it('counts UTF-8 bytes', () => {
    expect(carriedBytes('zażółć')).toBe(10)
  })
})

describe('the formatted JSON view', () => {
  it('indents a complete document and keeps every literal as recorded', () => {
    const text = '{"big":12345678901234567890,"huge":1e999,"neg":-0.0,"f":1.50,"s":"a\\u00e9\\n","z":[],"o":{},"t":[true,false,null],"nested":{"k":[1,{"x":2}]}}'
    expect(formatJson(text)).toBe(
      ['{', '  "big": 12345678901234567890,', '  "huge": 1e999,', '  "neg": -0.0,', '  "f": 1.50,', '  "s": "a\\u00e9\\n",', '  "z": [],', '  "o": {},', '  "t": [', '    true,', '    false,', '    null', '  ],', '  "nested": {', '    "k": [', '      1,', '      {', '        "x": 2', '      }', '    ]', '  }', '}'].join('\n'),
    )
  })

  it('keeps key order and duplicate keys as recorded', () => {
    expect(formatJson('{"b":1,"a":2,"b":3,"2":4,"1":5}')).toBe('{\n  "b": 1,\n  "a": 2,\n  "b": 3,\n  "2": 4,\n  "1": 5\n}')
  })

  it('is not offered when the text is not one complete JSON document', () => {
    for (const text of ['', '{"a":', '{"a":1} trailing', '{"a":1}{"b":2}', "{'a':1}", '{"a":1,}', '[1,2', 'NaN', '{"a":01}', '"unterminated', '{"a":"\u0001"}']) expect(formatJson(text), text).toBeNull()
  })

  it('is not offered when there is nothing to format, or the document is deeper than it will indent', () => {
    expect(formatJson('12345678901234567890')).toBeNull()
    expect(formatJson('"text"')).toBeNull()
    expect(formatJson('[]')).toBeNull()
    expect(formatJson('['.repeat(40) + ']'.repeat(40))).toBeNull()
    expect(formatJson('['.repeat(10_000) + ']'.repeat(10_000))).toBeNull()
  })

  it('does not treat bidirectional controls inside a string as anything but text to be marked later', () => {
    const formatted = formatJson('{"k":"a\u202Eb"}')!
    expect(formatted).toContain('\u202E')
    expect(displayText(formatted)).toContain('\\u202E')
    expect(displayText(formatted)).not.toContain('\u202E')
  })
})

describe('Copy', () => {
  it('writes exactly the text it is given and reports success', async () => {
    const written: string[] = []
    expect(await copyDisplayed('a\\u202Eb', async (s) => void written.push(s))).toBe(true)
    expect(written).toEqual(['a\\u202Eb'])
  })

  it('reports a refusal without throwing', async () => {
    expect(await copyDisplayed('x', async () => Promise.reject(new Error('denied')))).toBe(false)
    expect(await copyDisplayed('x', () => {
      throw new Error('no clipboard')
    })).toBe(false)
  })

  it('is never started by rendering', () => {
    const calls: string[] = []
    const real = Object.getOwnPropertyDescriptor(globalThis, 'navigator')
    Object.defineProperty(globalThis, 'navigator', { value: { clipboard: { writeText: (s: string) => (calls.push(s), Promise.resolve()) } }, configurable: true })
    try {
      html(FAST)
    } finally {
      if (real) Object.defineProperty(globalThis, 'navigator', real)
      else delete (globalThis as Record<string, unknown>).navigator
    }
    expect(calls).toEqual([])
  })
})

describe('edge inspector sources', () => {
  const read = (file: string) => readFileSync(new URL(`../src/editor/${file}`, import.meta.url), 'utf8')
  it('render values as text only: no markup injection, link, open action, storage or console', () => {
    for (const file of ['EdgeInspector.tsx', 'EdgeValueBox.tsx', 'valueText.ts', 'edgeValue.ts']) {
      expect(read(file), file).not.toMatch(/dangerouslySetInnerHTML|innerHTML|href=|window\.open|<a\s|localStorage|sessionStorage|indexedDB|console\.|fetch\(|XMLHttpRequest|shell\.openExternal/)
    }
  })

  it('call the clipboard only inside the explicit Copy handler', () => {
    const source = read('EdgeValueBox.tsx')
    expect(source.match(/clipboard/g)).toHaveLength(1)
    expect(source).toContain('onClick={copy}')
    expect(source).not.toMatch(/useEffect/)
  })
})
