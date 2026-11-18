import type { Meta, StoryObj } from '@storybook/react-vite'
import type { BridgeData } from './storybook/bridge'
import { allAvailable, clisFound, codexFoundOnPath, codexUnsupportedVersion, detectionError, gitMissing, linux, windows } from './storybook/capabilityFixtures'
import { Welcome } from './welcome'

const meta: Meta = { title: 'Screens/Welcome' }
export default meta
type S = StoryObj

const noop = () => undefined
const story = (capabilities: BridgeData['capabilities'], extra: Partial<BridgeData> = {}, play?: S['play']): S => ({
  parameters: { bridge: { runs: [], capabilities, ...extra } satisfies BridgeData },
  render: () => <Welcome onTemplate={noop} onOpen={noop} onSkip={noop} />,
  play,
})

export const AllAvailable: S = story({ report: allAvailable })
export const GitMissing: S = story({ report: gitMissing })
export const CodexNotConfigured: S = story({ report: linux })
export const CodexFoundOnPath: S = story({ report: codexFoundOnPath })
export const AgentClisFound: S = story({ report: clisFound })
export const CodexUnsupportedVersion: S = story({ report: codexUnsupportedVersion })
export const Windows: S = story({ report: windows })
export const DetectionError: S = story({ report: detectionError })
export const Checking: S = story({ getHangs: true })
export const Rechecking: S = story({ report: linux, checkHangs: true }, {}, async ({ canvasElement }) => {
  for (let i = 0; i < 40 && !canvasElement.querySelector('[data-testid="cap-git"]'); i++) await new Promise((r) => setTimeout(r, 25))
  canvasElement.querySelector<HTMLButtonElement>('[data-testid="check-again"]')?.click()
  await new Promise((r) => setTimeout(r, 100))
})
export const RequestFailed: S = story({ status: 503 })
export const EngineDisconnected: S = story(undefined, { unreachable: true })
