import type { Meta, StoryObj } from '@storybook/react-vite'
import configErrorOpen from '../../test/fixtures/graph-open-config-error.json'
import pairOpen from '../../test/fixtures/graph-open-pair.json'
import parseErrorOpen from '../../test/fixtures/graph-open-parse-error.json'
import parseErrorValidate from '../../test/fixtures/graph-validate-parse-error.json'
import pairValidate from '../../test/fixtures/graph-validate-pair.json'
import type { GraphStory } from '../storybook/bridge'
import { EditorScreen } from './EditorScreen'
import type { OpenResponse, ValidateResponse } from './types'
import { useGraphDocument } from './useGraphDocument'

// The editor's document states. Every server answer is recorded from the real
// graph document API (scripts/record-graph-fixtures.cjs); typing and clicking
// are driven through the real components.
const meta: Meta = { title: 'Screens/Editor' }
export default meta
type S = StoryObj

function Harness() {
  const g = useGraphDocument()
  return <EditorScreen g={g} />
}

const pair = pairOpen as unknown as OpenResponse
const validOrBroken = (text: string): ValidateResponse => ((text.includes('id: [') ? parseErrorValidate : pairValidate) as unknown as ValidateResponse)
const base: GraphStory = { opened: pair, validate: validOrBroken, recent: [{ id: 'r1', label: 'fix-issue.yaml' }, { id: 'r2', label: 'repo-audit.yaml' }] }

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function until(root: Element, selector: string, ms = 4000): Promise<Element> {
  const end = Date.now() + ms
  for (;;) {
    const el = root.querySelector(selector)
    if (el) return el
    if (Date.now() > end) throw new Error(`no ${selector}`)
    await wait(25)
  }
}
const click = async (root: Element, testId: string) => (await until(root, `[data-testid="${testId}"]`) as HTMLElement).click()
async function typeIn(root: Element, text: string) {
  const content = (await until(root, '.cm-content')) as HTMLElement
  content.focus()
  document.execCommand('insertText', false, text)
  await wait(50)
}
const open = async (root: Element) => {
  await click(root, 'editor-open')
  await until(root, '.cm-content')
}

const story = (graph: GraphStory, play?: (root: Element) => Promise<void>): S => ({
  parameters: { bridge: { runs: [], graph } },
  render: () => <Harness />,
  play: play ? async ({ canvasElement }) => play(canvasElement) : undefined,
})

export const Empty: S = story(base)
export const EmptyNoRecent: S = story({ ...base, recent: [] })
export const Opened: S = story(base, open)
export const OpenedConfigError: S = story({ ...base, opened: configErrorOpen as unknown as OpenResponse }, open)
export const OpenedParseError: S = story({ ...base, opened: parseErrorOpen as unknown as OpenResponse }, open)
export const Unsaved: S = story(base, async (root) => {
  await open(root)
  await typeIn(root, '# a note\n')
})
export const YamlErrorWhileTyping: S = story(base, async (root) => {
  await open(root)
  await typeIn(root, 'id: [\n')
  await wait(700)
})
export const NewGraph: S = story(base, async (root) => {
  await click(root, 'editor-new')
  await until(root, '.cm-content')
})
export const SaveConflict: S = story({ ...base, save: 'conflict' }, async (root) => {
  await open(root)
  await typeIn(root, '# mine\n')
  await click(root, 'editor-save')
  await until(root, '[data-testid="conflict-banner"]')
})
export const FileGone: S = story({ ...base, save: 'gone' }, async (root) => {
  await open(root)
  await typeIn(root, '# mine\n')
  await click(root, 'editor-save')
  await until(root, '[data-testid="conflict-banner"]')
})
export const SaveFailed: S = story({ ...base, save: 'error' }, async (root) => {
  await open(root)
  await typeIn(root, '# mine\n')
  await click(root, 'editor-save')
  await until(root, '[data-testid="editor-error"]')
})
export const LeavePrompt: S = story(base, async (root) => {
  await open(root)
  await typeIn(root, '# unsaved\n')
  await click(root, 'editor-open')
  await until(root, '[data-testid="leave-prompt"]')
})
export const OpenFailed: S = story({ ...base, openError: 'the file does not exist' }, async (root) => {
  await click(root, 'editor-open')
  await until(root, '[data-testid="editor-error"]')
})
