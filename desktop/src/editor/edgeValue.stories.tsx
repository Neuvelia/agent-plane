import type { Meta, StoryObj } from '@storybook/react-vite'
import demoApproveSchema9 from '../../test/fixtures/demo-approve.json'
import demoRejectSchema9 from '../../test/fixtures/demo-reject.json'
import valuesStream from '../../test/fixtures/demo-approve-values.json'
import demoOpen from '../../test/fixtures/graph-open-demo.json'
import pairValidate from '../../test/fixtures/graph-validate-pair.json'
import type { BridgeRun, GraphStory, StreamEnding } from '../storybook/bridge'
import { demoGraph, demoYaml } from '../storybook/fixtures'
import type { ApEvent } from '../types'
import { EditorScreen } from './EditorScreen'
import type { OpenResponse, ValidateResponse } from './types'
import { useGraphDocument } from './useGraphDocument'

// The edge inspector's states (F2.3 requirement 17). Every run is a recorded event stream of
// the demo graph: demo-approve-values.json (schema 10, recording on), demo-approve.json and
// demo-reject.json (schema 9). A story that needs another state changes or drops ONE value
// event of that stream, as a run with another input or recording off would have recorded it.
const meta: Meta = { title: 'Screens/Edge inspector' }
export default meta
type S = StoryObj

function Harness() {
  const g = useGraphDocument()
  return <EditorScreen g={g} onRun={() => undefined} />
}

const VERSION = (demoOpen as unknown as { version: string }).version
const recorded = valuesStream as unknown as ApEvent[]
const graph: GraphStory = { opened: demoOpen as unknown as OpenResponse, validate: () => pairValidate as unknown as ValidateResponse, recent: [] }

const FAST = { from: 'fast.outputs.stdout', to: 'approve.inputs.summary' }
const EVIDENCE = { from: 'flaky.outputs.json', to: 'approve.inputs.evidence' }
const DECISION = { from: 'approve.outputs.decision', to: 'apply.inputs.decision' }
type Edge = typeof FAST

const isValue = (port: string) => (e: ApEvent) => e.type === 'value_recorded' && e.payload.port === port
const patched = (port: string, payload: Record<string, unknown>): ApEvent[] => recorded.map((e) => (isValue(port)(e) ? { ...e, payload: { ...e.payload, ...payload } } : e))
const upTo = (events: ApEvent[], pred: (e: ApEvent) => boolean): ApEvent[] => events.slice(0, events.findIndex(pred) + 1)

interface RunOptions {
  /** The version the run's summary reports; null leaves it out, as an older store does. */
  version?: string | null
  ending?: StreamEnding
  status?: string
  unreadable?: boolean
}
function run(events: ApEvent[], o: RunOptions = {}): BridgeRun {
  const first = events[0]!
  return {
    summary: { run_id: first.run_id, graph_id: 'demo-vertical', started_at: first.timestamp, status: o.status ?? 'succeeded', event_count: events.length, graph_version: o.version === null ? undefined : (o.version ?? VERSION) },
    graph: demoGraph,
    yaml: demoYaml,
    events,
    ending: o.ending ?? 'end',
    unreadable: o.unreadable,
  }
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function until(root: Element, selector: string, ms = 5000): Promise<Element> {
  const end = Date.now() + ms
  for (;;) {
    const el = root.querySelector(selector)
    if (el) return el
    if (Date.now() > end) throw new Error(`no ${selector}`)
    await wait(25)
  }
}
const click = async (root: Element, testId: string) => ((await until(root, `[data-testid="${testId}"]`)) as HTMLElement).click()

async function pick(root: Element, edge: Edge) {
  await until(root, '[data-testid="editor-open"]')
  await click(root, 'editor-open')
  const row = (await until(root, `[data-testid="binding-select"][data-from="${edge.from}"][data-to="${edge.to}"]`)) as HTMLElement
  row.click()
}

/** Opens the demo graph, selects an edge and waits for the state the inspector should reach. */
const story = (runs: BridgeRun[], edge: Edge, state: string, more?: (root: Element) => Promise<void>, recording?: boolean): S => ({
  parameters: { bridge: { runs, graph, recording: recording ? { enabled: true, redactEnv: [], active: true, missing: [] } : undefined } },
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await pick(canvasElement, edge)
    await until(canvasElement, `[data-testid="edge-value-state"][data-state="${state}"]`)
    await more?.(canvasElement)
    // The inspector sits below the canvas; bring the state this story is about into view.
    canvasElement.querySelector('[data-testid="edge-inspector"]')?.scrollIntoView({ block: 'start' })
    await wait(150)
  },
})

// ---- the ten states of requirement 17, in order of precedence ---------------------------------
export const NoRun: S = story([], FAST, 'no_run')
export const RunUnavailable: S = story([run(recorded, { unreadable: true })], FAST, 'run_unavailable')
export const DifferentVersionNoneChosen: S = story([run(recorded, { version: 'sha256-v0:another-version' })], FAST, 'different_version')
export const DifferentVersionNeedsConfirmation: S = story([run(recorded, { version: null })], FAST, 'different_version', async (root) => {
  const select = (await until(root, '[data-testid="value-picker-select"]')) as HTMLSelectElement
  Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(select, recorded[0]!.run_id)
  select.dispatchEvent(new Event('change', { bubbles: true }))
  await until(root, '[data-testid="value-picker-confirm-button"]')
})
export const DifferentVersionConfirmed: S = story([run(recorded, { version: 'sha256-v0:another-version' })], FAST, 'different_version', async (root) => {
  const select = (await until(root, '[data-testid="value-picker-select"]')) as HTMLSelectElement
  Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(select, recorded[0]!.run_id)
  select.dispatchEvent(new Event('change', { bubbles: true }))
  await click(root, 'value-picker-confirm-button')
  await until(root, '[data-testid="edge-value-state"][data-state="value"]')
})
export const NodeNotFinished: S = story([run(upTo(recorded, (e) => e.type === 'node_started' && e.node_id === 'fast'), { ending: 'open', status: 'running' })], FAST, 'node_not_finished')
export const NodeWaitingForPerson: S = story([run(upTo(recorded, (e) => e.type === 'approval_requested'), { ending: 'open', status: 'running' })], DECISION, 'node_not_finished')
export const NodeFailed: S = story([run(demoRejectSchema9 as unknown as ApEvent[], { status: 'failed' })], DECISION, 'node_failed_or_skipped')
export const OlderRun: S = story([run(demoApproveSchema9 as unknown as ApEvent[])], FAST, 'older_run')
export const NotRecorded: S = story([run(recorded.filter((e) => e.type !== 'value_recorded'))], FAST, 'not_recorded')
export const NoValueForThisPort: S = story([run(recorded.filter((e) => !isValue('json')(e)))], EVIDENCE, 'no_value_for_port')
export const ValueNotCarried: S = story([run(patched('stdout', { value: undefined, truncated: true, size_bytes: 20000 }))], FAST, 'value_not_carried')
export const ValueString: S = story([run(recorded)], FAST, 'value', undefined, true)

// ---- what a value can look like ----------------------------------------------------------------
const tall = Array.from({ length: 120 }, (_, i) => `line ${i + 1}: ${'recorded text '.repeat(6)}`).join('\n')
const hostile = ['visible', String.fromCharCode(0x202e), 'reversed', String.fromCharCode(0x2066), String.fromCharCode(0x07), 'bell', String.fromCharCode(0x9f)].join('')
export const ValueJsonFormatted: S = story([run(patched('json', { value: '{"ready":true,"attempts":[1,2],"id":12345678901234567890,"huge":1e999}\n', size_bytes: 68 }))], EVIDENCE, 'value')
export const ValueJsonRaw: S = story([run(patched('json', { value: '{"ready":true,"attempts":[1,2]}\n', size_bytes: 31 }))], EVIDENCE, 'value', async (root) => click(root, 'edge-value-raw'))
export const ValueNotJson: S = story([run(patched('json', { value: '{"ready":', size_bytes: 9 }))], EVIDENCE, 'value')
export const ValueTruncated: S = story([run(patched('stdout', { value: tall.slice(0, 2000), truncated: true, size_bytes: 20000 }))], FAST, 'value')
export const ValueRedacted: S = story([run(patched('stdout', { value: 'token=[redacted] and the rest', redacted: true, size_bytes: 29 }))], FAST, 'value')
export const ValueLong: S = story([run(patched('stdout', { value: tall, size_bytes: tall.length }))], FAST, 'value')
export const ValueHiddenCharacters: S = story([run(patched('stdout', { value: hostile, size_bytes: hostile.length }))], FAST, 'value')
export const ValueEmpty: S = story([run(patched('stdout', { value: '', size_bytes: 0 }))], FAST, 'value')
export const ValueArtifactReference: S = story([run(patched('stdout', { type: 'artifact_ref', value: 'artifact://build/output.tar', size_bytes: 27 }))], FAST, 'value')

// ---- a run in progress and a lost connection (requirement 18) ----------------------------------
export const LiveRunValueArrived: S = story([run(upTo(recorded, (e) => e.type === 'approval_requested'), { ending: 'open', status: 'running' })], FAST, 'value')
export const ConnectionLost: S = story([run(upTo(recorded, (e) => e.type === 'approval_requested'), { ending: 'lost', status: 'running' })], FAST, 'value', async (root) => {
  await until(root, '[data-testid="value-picker-lost"]')
  await until(root, '[data-testid="edge-value-incomplete"]')
})
export const ConnectionLostBeforeValue: S = story([run(upTo(recorded, (e) => e.type === 'node_started' && e.node_id === 'fast'), { ending: 'lost', status: 'running' })], FAST, 'node_not_finished', async (root) => {
  await until(root, '[data-testid="value-picker-lost"]')
})
