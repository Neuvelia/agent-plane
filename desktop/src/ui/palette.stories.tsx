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

/** Goes to the dashboard (G then D), opens the palette from Search, then runs the step. */
const open =
  (step?: (input: HTMLInputElement) => Promise<void> | void): S['play'] =>
  async ({ canvasElement }) => {
    await until(canvasElement, '[data-testid="palette-open"]')
    // Let the shell's effects (its keyboard listener) run after the first render.
    await wait(100)
    press(window, 'g')
    press(window, 'd')
    await wait(150)
    ;(await until<HTMLButtonElement>(canvasElement, '[data-testid="palette-open"]')).click()
    const input = await until<HTMLInputElement>(canvasElement, '[data-testid="palette-input"]')
    // Runs arrive after the palette opens.
    await wait(200)
    await step?.(input)
    await wait(250)
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

export const Open: S = story(busy, open())
export const KeyboardNavigation: S = story(
  busy,
  open((input) => {
    press(input, 'ArrowDown')
    press(input, 'ArrowDown')
    press(input, 'ArrowDown')
  }),
)
export const Filtered: S = story(
  busy,
  open((input) => type(input, 'set')),
)
export const ActionsOnly: S = story(
  busy,
  open((input) => {
    press(input, 'Tab')
  }),
)
export const RunIds: S = story(
  busy,
  open((input) => type(input, `#${waitingForHuman[0]!.run_id.slice(0, 4)}`)),
)
export const NothingMatches: S = story(
  busy,
  open((input) => type(input, 'zzz')),
)
export const RunsLoading: S = story({ runs: [], hang: true }, open())
export const RunsUnavailable: S = story({ runs: [], unreachable: true }, open())
