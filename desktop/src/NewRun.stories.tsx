import type { Meta, StoryObj } from '@storybook/react-vite'
import configErrorOpen from '../test/fixtures/graph-open-config-error.json'
import demoOpen from '../test/fixtures/graph-open-demo.json'
import fixIssueOpen from '../test/fixtures/graph-open-fix-issue.json'
import pairOpen from '../test/fixtures/graph-open-pair.json'
import inputsOpen from '../test/fixtures/graph-open-with-inputs.json'
import configErrorValidate from '../test/fixtures/graph-validate-config-error.json'
import fixIssueValidate from '../test/fixtures/graph-validate-fix-issue.json'
import pairValidate from '../test/fixtures/graph-validate-pair.json'
import inputsValidate from '../test/fixtures/graph-validate-with-inputs.json'
import type { OpenResponse } from './editor/types'
import { NewRun } from './NewRun'
import { allAvailable, linux } from './storybook/capabilityFixtures'
import type { BridgeData } from './storybook/bridge'

// F2.5c: New run with preflight. Graph-document and validation answers are
// recorded from the real engine (test/fixtures/graph-*.json); the capability
// reports are those of the Welcome stories.

const meta: Meta = { title: 'Screens/New run' }
export default meta
type S = StoryObj

const noop = () => undefined
// The screen fills the content area it is given, as in the app.
const render = () => (
  <div style={{ minHeight: 'calc(100vh - 16px)', display: 'flex', flexDirection: 'column' }}>
    <NewRun onStarted={noop} />
  </div>
)
const settle = () => new Promise((r) => setTimeout(r, 120))
const choose: S['play'] = async ({ canvasElement }) => {
  canvasElement.querySelector<HTMLButtonElement>('[data-testid="choose"]')?.click()
  await settle()
}
// Types into a React-controlled field the way a person does.
const type = (canvasElement: HTMLElement, value: string) => {
  const input = canvasElement.querySelector<HTMLInputElement>('[data-testid="run-input"]')
  if (!input) return
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
const chooseAndType =
  (value: string): S['play'] =>
  async (ctx) => {
    await choose(ctx)
    type(ctx.canvasElement, value)
    await settle()
  }
const story = (bridge: BridgeData, play?: S['play']): S => ({ parameters: { bridge }, render, play })
const opened = (o: unknown) => o as OpenResponse

/** Nothing chosen and nothing opened before. */
export const NoGraph = story({ runs: [] })
/** Recently opened graphs to pick from. */
export const RecentGraphs = story({ runs: [], graph: { opened: opened(demoOpen), recent: [{ id: 'r1', label: 'demo-vertical.yaml' }, { id: 'r2', label: 'release-notes.yaml' }, { id: 'r3', label: 'docs-refresh.yaml' }] } })
/** The artboard: an Agent graph in the shared working tree, Codex available, the issue filled. */
export const AgentGraphReady = story({ runs: [], graph: { opened: opened(fixIssueOpen) }, validate: fixIssueValidate, capabilities: { report: allAvailable } }, chooseAndType('#142 Range parser drops an open upper bound'))
/** The same graph before its required input is filled, on an engine without a configured Codex. */
export const AgentGraphCodexNotConfigured = story({ runs: [], graph: { opened: opened(fixIssueOpen) }, validate: fixIssueValidate, capabilities: { report: linux } }, choose)
/** The capability check never answers. */
export const AgentGraphCheckingCodex = story({ runs: [], graph: { opened: opened(fixIssueOpen) }, validate: fixIssueValidate, capabilities: { report: allAvailable, getHangs: true } }, choose)
/** A shell graph with no inputs: ready to run. */
export const Ready = story({ runs: [], graph: { opened: opened(demoOpen) } }, choose)
/** A required input, still empty: a warning, not a block (the engine decides when the run starts). */
export const InputRequired = story({ runs: [], graph: { opened: opened(inputsOpen) }, validate: inputsValidate }, choose)
export const InputFilled = story({ runs: [], graph: { opened: opened(inputsOpen) }, validate: inputsValidate }, chooseAndType('release notes'))
/** The engine's warning never blocks. */
export const Warnings = story({ runs: [], graph: { opened: opened(pairOpen) }, validate: pairValidate }, choose)
/** The engine's error blocks the run. */
export const Blocked = story({ runs: [], graph: { opened: opened(configErrorOpen) }, validate: configErrorValidate }, choose)
/** Validation never answers. */
export const Validating = story({ runs: [], pickedGraph: 'graph.yaml', hang: true }, choose)
/** The engine cannot be reached to check the chosen file. */
export const EngineUnreachable = story({ runs: [], pickedGraph: 'graph.yaml', unreachable: true }, choose)
/** Start refused by the engine: its message and diagnostics join the preflight. */
export const StartFails = story({ runs: [], graph: { opened: opened(demoOpen), run: 'error' } }, async (ctx) => {
  await choose(ctx)
  ctx.canvasElement.querySelector<HTMLButtonElement>('[data-testid="run"]')?.click()
  await settle()
})
