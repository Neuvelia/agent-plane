import type { Meta, StoryObj } from '@storybook/react-vite'
import agentValidate from '../../test/fixtures/graph-validate-template-agent-scoped-approval.json'
import type { BridgeData, GraphStory } from '../storybook/bridge'
import { allAvailable, linux, windows } from '../storybook/capabilityFixtures'
import { EditorScreen } from './EditorScreen'
import type { ValidateResponse } from './types'
import { useGraphDocument } from './useGraphDocument'

// The New graph picker (F2.12). Capability answers are recorded reports (capabilityFixtures);
// the validation of the agent template draft is the engine's recorded answer
// (scripts/record-graph-fixtures.cjs, AP_RECORD=templates). Choices go through the real components.
const meta: Meta = { title: 'Screens/Template picker' }
export default meta
type S = StoryObj

function Harness() {
  const g = useGraphDocument()
  return <EditorScreen g={g} onRun={() => undefined} />
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function until(root: Element, selector: string, ms = 4000): Promise<HTMLElement> {
  const end = Date.now() + ms
  for (;;) {
    const el = root.querySelector<HTMLElement>(selector)
    if (el) return el
    if (Date.now() > end) throw new Error(`no ${selector}`)
    await wait(25)
  }
}
const click = async (root: Element, testId: string) => (await until(root, `[data-testid="${testId}"]`)).click()
const picker = async (root: Element) => {
  await click(root, 'editor-new')
  await until(root, '[data-testid="template-picker"]')
  // Notices read the report, which arrives after the dialog opens.
  await wait(150)
}

const story = (capabilities: BridgeData['capabilities'], play?: (root: Element) => Promise<void>, validate?: GraphStory['validate']): S => ({
  parameters: { bridge: { runs: [], capabilities, graph: { validate, recent: [] } } satisfies BridgeData },
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await (play ?? picker)(canvasElement)
  },
})

export const Default: S = story({ report: allAvailable })
export const WindowsReport: S = story({ report: windows })
export const NoReport: S = story({ status: 503 })
export const CodexNotConfigured: S = story({ report: linux }, async (root) => {
  await picker(root)
  await click(root, 'template-agent-scoped-approval')
})
export const AgentDraftShowsPlaceholder: S = story(
  { report: linux },
  async (root) => {
    await picker(root)
    await click(root, 'template-agent-scoped-approval')
    await click(root, 'picker-create')
    await until(root, '[data-testid="issue"]', 6000)
  },
  () => agentValidate as unknown as ValidateResponse,
)
