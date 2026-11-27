import type { Meta, StoryObj } from '@storybook/react-vite'
import type { BridgeData } from '../storybook/bridge'
import { allAvailable, codexFoundOnPath, codexUnsupportedVersion, detectionError, linux } from '../storybook/capabilityFixtures'
import { Settings } from '.'

// F2.5f Settings. Preferences come from the in-memory bridge; the capability
// report is the recorded Linux one or a row-level variant of it. Plays wait for
// the element or state they need, never for a fixed time.
const meta: Meta = { title: 'Screens/Settings' }
export default meta
type S = StoryObj
type Ctx = Parameters<NonNullable<S['play']>>[0]

/** Resolves with the first element matching `selector` (that also passes `ready`), checking each frame for up to 4 s. */
async function until<E extends Element>(root: Element, selector: string, ready: (e: E) => boolean = () => true): Promise<E> {
  const end = Date.now() + 4000
  for (;;) {
    const el = root.querySelector<E>(selector)
    if (el && ready(el)) return el
    if (Date.now() > end) throw new Error(`story never showed ${selector}`)
    await new Promise((r) => setTimeout(r, 16))
  }
}
const enabled = (e: HTMLInputElement | HTMLButtonElement) => !e.disabled
const open = (section: string, shows: string): S['play'] => async ({ canvasElement }: Ctx) => {
  ;(await until<HTMLButtonElement>(canvasElement, `[data-testid="settings-nav-${section}"]`)).click()
  await until(canvasElement, shows)
}
const toggleThen = (shows: string): S['play'] => async ({ canvasElement }: Ctx) => {
  ;(await until<HTMLInputElement>(canvasElement, '[data-testid="values-record"]', enabled)).click()
  await until(canvasElement, shows)
}
const story = (bridge: Partial<BridgeData>, play?: S['play']): S => ({
  parameters: { bridge: { runs: [], capabilities: { report: linux }, ...bridge } satisfies BridgeData },
  render: () => <Settings />,
  play,
})
/** Types into the names field the way a person does, so React sees the change. */
function type(input: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

export const Language: S = story({})
// "Record run values" (ADR-0023): on for this session, off because a
// configured variable is not set, and the preference failing to load or save.
export const RecordingOn: S = story({ recording: { enabled: true, redactEnv: ['GITHUB_TOKEN'], active: true, missing: [] } })
export const RecordingMissingVariable: S = story({ recording: { enabled: true, redactEnv: ['GITHUB_TOKEN', 'NPM_TOKEN'], active: false, missing: ['NPM_TOKEN'] } })
export const RecordingSaved: S = story({}, toggleThen('[data-testid="values-saved"]'))
export const RecordingSaveFails: S = story({ recordingSaveFails: true }, toggleThen('[data-testid="values-save-failed"]'))
export const RecordingSaveThrows: S = story({ recordingSaveThrows: true }, toggleThen('[data-testid="values-save-failed"]'))
export const RecordingInvalidName: S = story({}, async ({ canvasElement }: Ctx) => {
  type(await until<HTMLInputElement>(canvasElement, '[data-testid="values-redact-env"]', enabled), 'GITHUB_TOKEN A=B')
  ;(await until<HTMLButtonElement>(canvasElement, '[data-testid="values-save"]')).click()
  await until(canvasElement, '[data-testid="values-invalid"]')
})
// A typed, unsaved draft survives the switch saving.
export const RecordingUnsavedDraftKept: S = story({ recording: { enabled: false, redactEnv: ['GITHUB_TOKEN'], active: false, missing: [] } }, async (ctx: Ctx) => {
  type(await until<HTMLInputElement>(ctx.canvasElement, '[data-testid="values-redact-env"]', enabled), 'GITHUB_TOKEN NPM_TOKEN')
  await toggleThen('[data-testid="values-saved"]')!(ctx)
  await until(ctx.canvasElement, '[data-testid="values-unsaved"]')
})
export const RecordingLoadFails: S = story({ recordingLoadFails: true })
export const RecordingDetailsOpen: S = story({}, async ({ canvasElement }: Ctx) => {
  ;(await until<HTMLElement>(canvasElement, '.st-more summary')).click()
  await until(canvasElement, '.st-more[open]')
})
export const JumpToEngine: S = story({}, open('engine', '[data-testid="settings-nav-engine"][aria-current="true"]'))
export const EngineChecking: S = story({ capabilities: { getHangs: true } })
export const EngineDisconnected: S = story({ unreachable: true })
const agent = (id: string) => `[data-testid="agent-${id}"]`
export const AgentsCodexAvailable: S = story({ capabilities: { report: allAvailable } }, open('agents', agent('codex')))
export const AgentsCodexNotConfigured: S = story({}, open('agents', agent('codex')))
export const AgentsCodexFoundOnPath: S = story({ capabilities: { report: codexFoundOnPath } }, open('agents', agent('codex')))
export const AgentsCodexUnsupportedVersion: S = story({ capabilities: { report: codexUnsupportedVersion } }, open('agents', agent('codex')))
export const AgentsDetectionError: S = story({ capabilities: { report: detectionError } }, open('agents', agent('codex')))
export const AgentsRechecking: S = story({ capabilities: { report: allAvailable, checkHangs: true } }, async (ctx: Ctx) => {
  await open('agents', agent('codex'))!(ctx)
  ;(await until<HTMLButtonElement>(ctx.canvasElement, '[data-testid="agent-check-codex"]')).click()
  await until(ctx.canvasElement, '[data-testid="agents-refreshing"]')
})
export const AgentsRequestFailed: S = story({ capabilities: { status: 503 } }, open('agents', '[data-testid="agents-failed"]'))
export const AgentsEngineDisconnected: S = story({ unreachable: true }, open('agents', '[data-testid="agents-disconnected"]'))
export const About: S = story({ capabilities: { report: allAvailable } }, open('about', '[data-testid="about-engine"]'))
export const AboutEngineDisconnected: S = story({ unreachable: true }, open('about', '[data-testid="about-engine"]'))
