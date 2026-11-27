import type { Meta, StoryObj } from '@storybook/react-vite'
import App from '../App'
import type { BridgeData, BridgeRun, StreamEnding } from '../storybook/bridge'
import { approvalsOf, demoGraph, demoReject, demoYaml, exhaustedGraph, exhaustedRetries, exhaustedYaml, summaryOf, waitingForHuman } from '../storybook/fixtures'
import type { ApEvent } from '../types'

// F2.5b command palette over the real app shell. Runs are recorded event streams
// (storybook/fixtures); every step goes through the real components: the Search
// button opens the palette, typing and arrow keys drive it.
const meta: Meta = { title: 'Screens/Command palette' }
export default meta
type S = StoryObj

function run(events: ApEvent[], status: string, ending: StreamEnding, demo = true): BridgeRun {
  return { summary: summaryOf(events, status), graph: demo ? demoGraph : exhaustedGraph, yaml: demo ? demoYaml : exhaustedYaml, events, ending }
}
const busy: BridgeData = {
  runs: [run(waitingForHuman, 'running', 'open'), run(exhaustedRetries, 'failed', 'end', false), run(demoReject, 'succeeded', 'end')],
  approvals: approvalsOf(waitingForHuman),
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function until<T extends Element>(root: ParentNode, selector: string, ms = 4000): Promise<T> {
  const end = Date.now() + ms
  for (;;) {
    const el = root.querySelector<T>(selector)
    if (el) return el
    if (Date.now() > end) throw new Error(`no ${selector}`)
    await wait(25)
  }
}
const press = (target: EventTarget, key: string, init: KeyboardEventInit = {}) => target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }))
/** Sets a React-controlled field the way typing does. */
function type(input: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

/** Waits until a condition holds; a story whose interaction does not reach its state fails (the smoke reports it). */
async function expectState(what: string, ok: () => boolean, ms = 4000) {
  const end = Date.now() + ms
  while (!ok()) {
    if (Date.now() > end) throw new Error(`palette story: expected ${what}`)
    await wait(25)
  }
}
const selected = (root: ParentNode) => root.querySelector('[role="option"][aria-selected="true"]')?.getAttribute('data-testid')
const options = (root: ParentNode) => [...root.querySelectorAll('[role="option"]')].map((o) => o.getAttribute('data-testid'))

const RUN_ROW = '[data-testid^="palette-item-run:"]'
const RUNS_STATE = '[data-testid="palette-runs-state"]'

/**
 * Goes to the dashboard (G then D), opens the palette from Search, waits for `ready`
 * (the run rows, or the runs state line), then runs the step and checks where it ended.
 */
const open =
  (ready: string, step?: (input: HTMLInputElement, root: HTMLElement) => Promise<void> | void): S['play'] =>
  async ({ canvasElement }) => {
    await until(canvasElement, '[data-testid="palette-open"]')
    // The shell's keyboard listener is attached by an effect after the first render: repeat until it answers.
    await expectState('G D to reach the dashboard', () => {
      if (canvasElement.querySelector('[data-testid="dashboard"]')) return true
      press(window, 'g')
      press(window, 'd')
      return false
    })
    ;(await until<HTMLButtonElement>(canvasElement, '[data-testid="palette-open"]')).click()
    const input = await until<HTMLInputElement>(canvasElement, '[data-testid="palette-input"]')
    await until(canvasElement, ready)
    await step?.(input, canvasElement)
  }

const story = (bridge: BridgeData, play: S['play']): S => ({
  parameters: { bridge },
  render: () => (
    <div style={{ height: '100vh' }}>
      <App />
    </div>
  ),
  play,
})

export const Open: S = story(
  busy,
  open(RUN_ROW, (_, root) => expectState('New run active', () => selected(root) === 'palette-item-new-run')),
)
export const KeyboardNavigation: S = story(
  busy,
  open(RUN_ROW, async (input, root) => {
    press(input, 'ArrowDown')
    press(input, 'ArrowDown')
    press(input, 'ArrowDown')
    await expectState('the fourth row active', () => selected(root) === options(root)[3])
  }),
)
export const Filtered: S = story(
  busy,
  open(RUN_ROW, async (input, root) => {
    // A graph name: the same in every language.
    type(input, 'exhausted')
    const id = exhaustedRetries[0]!.run_id
    await expectState('only the exhausted-retries run', () => options(root).join() === `palette-item-run:${id}`)
  }),
)
export const ActionsOnly: S = story(
  busy,
  open(RUN_ROW, async (input, root) => {
    press(input, 'Tab')
    await expectState('">" and actions only', () => input.value === '>' && options(root).every((id) => ['palette-item-new-run', 'palette-item-open-graph', 'palette-item-approvals'].includes(id ?? '')))
  }),
)
export const RunIds: S = story(
  busy,
  open(RUN_ROW, async (input, root) => {
    const id = waitingForHuman[0]!.run_id
    type(input, `#${id.slice(0, 4)}`)
    await expectState('one run row', () => options(root).join() === `palette-item-run:${id}`)
  }),
)
export const NothingMatches: S = story(
  busy,
  open(RUN_ROW, async (input, root) => {
    type(input, 'zzz')
    await until(root, '[data-testid="palette-none"]')
  }),
)
export const RunsLoading: S = story({ runs: [], hang: true }, open(RUNS_STATE))
export const RunsUnavailable: S = story(
  { runs: [], unreachable: true },
  open(RUNS_STATE, (_, root) => expectState('the error line', () => root.querySelector(RUNS_STATE)?.classList.contains('bad') === true)),
)
