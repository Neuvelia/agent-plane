import type { Meta, StoryObj } from '@storybook/react-vite'
import type { BridgeData } from '../storybook/bridge'
import { allAvailable, codexFoundOnPath, detectionError, linux } from '../storybook/capabilityFixtures'
import { Settings } from '.'

// F2.5b Settings. Preferences come from the in-memory bridge; the capability
// report is the recorded Linux one or a row-level variant of it.
const meta: Meta = { title: 'Screens/Settings' }
export default meta
type S = StoryObj

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
const open = (section: string): S['play'] => async ({ canvasElement }) => {
  await wait(50)
  canvasElement.querySelector<HTMLButtonElement>(`[data-testid="settings-nav-${section}"]`)?.click()
  await wait(500)
}
const story = (bridge: Partial<BridgeData>, play?: S['play']): S => ({
  parameters: { bridge: { runs: [], capabilities: { report: linux }, ...bridge } satisfies BridgeData },
  render: () => <Settings />,
  play,
})

export const Language: S = story({})
// "Record run values" (ADR-0023): on for this session, off because a
// configured variable is not set, and the preference failing to load or save.
export const RecordingOn: S = story({ recording: { enabled: true, redactEnv: ['GITHUB_TOKEN'], active: true, missing: [] } })
export const RecordingMissingVariable: S = story({ recording: { enabled: true, redactEnv: ['GITHUB_TOKEN', 'NPM_TOKEN'], active: false, missing: ['NPM_TOKEN'] } })
export const RecordingSaveFails: S = story({ recordingSaveFails: true }, async ({ canvasElement }) => {
  await wait(50)
  canvasElement.querySelector<HTMLInputElement>('[data-testid="values-record"]')?.click()
  await wait(200)
})
export const RecordingSaved: S = story({}, async ({ canvasElement }) => {
  await wait(50)
  canvasElement.querySelector<HTMLInputElement>('[data-testid="values-record"]')?.click()
  await wait(200)
})
export const RecordingLoadFails: S = story({ recordingLoadFails: true })
export const RecordingDetailsOpen: S = story({}, async ({ canvasElement }) => {
  await wait(50)
  canvasElement.querySelector<HTMLElement>('.st-more summary')?.click()
  await wait(300)
})
export const JumpToEngine: S = story({}, open('engine'))
export const EngineChecking: S = story({ capabilities: { getHangs: true } })
export const EngineDisconnected: S = story({ unreachable: true })
export const AgentsCodexAvailable: S = story({ capabilities: { report: allAvailable } }, open('agents'))
export const AgentsCodexNotConfigured: S = story({}, open('agents'))
export const AgentsCodexFoundOnPath: S = story({ capabilities: { report: codexFoundOnPath } }, open('agents'))
export const AgentsDetectionError: S = story({ capabilities: { report: detectionError } }, open('agents'))
export const AgentsRechecking: S = story({ capabilities: { report: allAvailable, checkHangs: true } }, async (ctx) => {
  await open('agents')!(ctx)
  ctx.canvasElement.querySelector<HTMLButtonElement>('[data-testid="agent-check-codex"]')?.click()
  await wait(100)
})
export const AgentsRequestFailed: S = story({ capabilities: { status: 503 } }, open('agents'))
export const AgentsEngineDisconnected: S = story({ unreachable: true }, open('agents'))
export const About: S = story({ capabilities: { report: allAvailable } }, open('about'))
export const AboutEngineDisconnected: S = story({ unreachable: true }, open('about'))
