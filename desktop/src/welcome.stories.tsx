import type { Meta, StoryObj } from '@storybook/react-vite'
import App from './App'
import { WELCOME_KEY } from './capabilities'
import type { SetupStep } from './setup'
import type { BridgeData } from './storybook/bridge'
import { allAvailable, clisFound, codexFoundOnPath, codexUnsupportedVersion, detectionError, gitMissing, linux, shellMissing, windows } from './storybook/capabilityFixtures'
import { Welcome } from './welcome'

// Setup, revised (canvas artboards "Setup, revised" 1-4, checking and engine
// not connected). The capability states open on step 2, where the report is shown.
const meta: Meta = { title: 'Screens/Welcome' }
export default meta
type S = StoryObj

const noop = () => undefined
const frame = { height: 'calc(100vh - 16px)', display: 'flex', flexDirection: 'column' } as const
const story = (capabilities: BridgeData['capabilities'], extra: Partial<BridgeData> = {}, step: SetupStep = 'check', play?: S['play']): S => ({
  parameters: { bridge: { runs: [], capabilities, ...extra } satisfies BridgeData },
  render: () => (
    <div style={frame}>
      <Welcome onCreate={noop} onOpen={noop} onSkip={noop} initialStep={step} />
    </div>
  ),
  play,
})
const settled = async (root: HTMLElement, selector: string) => {
  for (let i = 0; i < 40 && !root.querySelector(selector); i++) await new Promise((r) => setTimeout(r, 25))
}

export const StepWelcome: S = story({ report: linux }, {}, 'welcome')
export const StepAgents: S = story({ report: linux }, {}, 'agents')
export const StepFirstGraph: S = story({ report: linux }, {}, 'graph')
// The F2.12 notices: commands that need Linux or macOS, and each tool a template needs.
export const StepFirstGraphWindows: S = story({ report: windows }, {}, 'graph', async ({ canvasElement }) => {
  await settled(canvasElement, '[data-testid="setup-needs"]')
})
export const StepFirstGraphBlankPicked: S = story({ report: linux }, {}, 'graph', async ({ canvasElement }) => {
  await settled(canvasElement, '[data-testid="setup-template-blank"]')
  canvasElement.querySelector<HTMLInputElement>('[data-testid="setup-template-blank"]')?.click()
})

export const AllAvailable: S = story({ report: allAvailable })
export const GitMissing: S = story({ report: gitMissing })
export const ShellMissing: S = story({ report: shellMissing })
export const CodexNotConfigured: S = story({ report: linux })
export const CodexFoundOnPath: S = story({ report: codexFoundOnPath })
export const AgentClisFound: S = story({ report: clisFound })
export const CodexUnsupportedVersion: S = story({ report: codexUnsupportedVersion })
export const Windows: S = story({ report: windows })
export const DetectionError: S = story({ report: detectionError })
export const Checking: S = story({ getHangs: true })
export const Rechecking: S = story({ report: linux, checkHangs: true }, {}, 'check', async ({ canvasElement }) => {
  await settled(canvasElement, '[data-testid="cap-git"]')
  canvasElement.querySelector<HTMLButtonElement>('[data-testid="check-again"]')?.click()
  await new Promise((r) => setTimeout(r, 100))
})
export const RequestFailed: S = story({ status: 503 })
export const EngineDisconnected: S = story(undefined, { unreachable: true })

// The whole window on first launch: the sidebar beside the setup, no bars.
const firstLaunch = () => {
  try {
    localStorage.removeItem(WELCOME_KEY)
  } catch {
    // Storage unavailable: the app opens setup anyway.
  }
}
const inApp = (bridge: BridgeData, play?: S['play']): S => ({
  parameters: { bridge },
  beforeEach: firstLaunch,
  render: () => (
    <div style={{ height: 'calc(100vh - 16px)' }}>
      <App />
    </div>
  ),
  play,
})
export const InAppFirstLaunch: S = inApp({ runs: [], capabilities: { report: linux } })
export const InAppEngineDisconnected: S = inApp({ runs: [], unreachable: true }, async ({ canvasElement }) => {
  await settled(canvasElement, '[data-testid="setup-progress-check"]')
  canvasElement.querySelector<HTMLButtonElement>('[data-testid="setup-progress-check"]')?.click()
})

// The canvas's light artboards ("Setup, revised" 1-4 and "checking" · light), with the theme pinned (F2.5c).
const light = (base: S): S => ({ ...base, parameters: { ...base.parameters, theme: 'light' } })
export const InAppFirstLaunchLight: S = light(InAppFirstLaunch)
export const StepCheckLight: S = light(CodexNotConfigured)
export const StepAgentsLight: S = light(StepAgents)
export const StepFirstGraphLight: S = light(StepFirstGraph)
export const CheckingLight: S = light(Checking)
