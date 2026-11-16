import type { Meta, StoryObj } from '@storybook/react-vite'
import configErrorOpen from '../../../test/fixtures/inspector-open-config-error.json'
import loopsOpen from '../../../test/fixtures/inspector-open-loops.json'
import shellOpen from '../../../test/fixtures/inspector-open-shell.json'
import typesOpen from '../../../test/fixtures/inspector-open-types.json'
import removePort from '../../../test/fixtures/inspector-edit-remove-port.json'
import setCommand from '../../../test/fixtures/inspector-edit-set-command.json'
import parseErrorValidate from '../../../test/fixtures/graph-validate-parse-error.json'
import editRefused from '../../../test/fixtures/graph-edit-refused.json'
import type { GraphStory } from '../../storybook/bridge'
import { EditorScreen } from '../EditorScreen'
import type { OpenResponse, ValidateResponse } from '../types'
import { useGraphDocument } from '../useGraphDocument'

// The node inspector (F2.2 requirement 15). Every engine answer is recorded from
// the real document API by cmd/agentplane/inspector_fixtures_test.go; selecting
// and clicking go through the real components.
const meta: Meta = { title: 'Screens/Inspector' }
export default meta
type S = StoryObj

function Harness() {
  const g = useGraphDocument()
  return <EditorScreen g={g} onRun={() => undefined} />
}

const as = (r: unknown) => r as OpenResponse
const graph = (opened: unknown, extra: Partial<GraphStory> = {}): GraphStory => ({
  opened: as(opened),
  validate: (text) => (text.includes('id: [') ? (parseErrorValidate as unknown as ValidateResponse) : (as(opened) as unknown as ValidateResponse)),
  edit: (ops) => ({ remove_port: removePort, set_config: setCommand })[ops[0]?.op ?? ''],
  ...extra,
})

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function until(root: Element, selector: string, ms = 4000): Promise<HTMLElement> {
  const end = Date.now() + ms
  for (;;) {
    const el = root.querySelector(selector)
    if (el) return el as HTMLElement
    if (Date.now() > end) throw new Error(`no ${selector}`)
    await wait(25)
  }
}
const click = async (root: Element, testId: string) => (await until(root, `[data-testid="${testId}"]`)).click()
async function inspect(root: Element, node: string) {
  await click(root, 'editor-open')
  await click(root, `list-node-${node}`)
  await until(root, '[data-testid="inspector-node"]')
  await wait(200)
}
const story = (g: GraphStory, play: (root: Element) => Promise<void>): S => ({
  parameters: { bridge: { runs: [], graph: g } },
  render: () => <Harness />,
  play: async ({ canvasElement }) => play(canvasElement),
})

export const NothingSelected: S = story(graph(shellOpen), async (root) => {
  await click(root, 'editor-open')
  await until(root, '[data-testid="inspector-empty"]')
})
export const ShellWithJsonPredicateLoop: S = story(graph(shellOpen), (root) => inspect(root, 'build'))
export const AdvancedSection: S = story(graph(shellOpen), async (root) => {
  await inspect(root, 'build')
  ;(await until(root, '[data-testid="advanced-entry-retries"]')).scrollIntoView()
})
export const LoopCommandGate: S = story(graph(loopsOpen), (root) => inspect(root, 'diffed'))
export const LoopJsonPredicateGate: S = story(graph(loopsOpen), (root) => inspect(root, 'status'))
export const LoopReportedFileScopeGate: S = story(graph(loopsOpen), (root) => inspect(root, 'files'))
export const AgentLoopChangedFilesScope: S = story(graph(loopsOpen), (root) => inspect(root, 'fix'))
export const AgentTaskFromInput: S = story(graph(typesOpen), (root) => inspect(root, 'agent'))
export const StandaloneGate: S = story(graph(typesOpen), (root) => inspect(root, 'scope'))
export const HumanNode: S = story(graph(typesOpen), (root) => inspect(root, 'approve'))
export const UnknownNodeType: S = story(graph(typesOpen), (root) => inspect(root, 'future'))
export const FieldWithDiagnostic: S = story(graph(configErrorOpen), (root) => inspect(root, 'work'))
export const LoopNeedsAnOutput: S = story(graph(shellOpen), async (root) => {
  await inspect(root, 'lint')
  await click(root, 'loop-enable')
  await until(root, '[data-testid="loop-no-outputs"]')
})
export const PortRemovalPreview: S = story(graph(shellOpen), async (root) => {
  await inspect(root, 'build')
  await click(root, 'remove-port-outputs-stdout')
  await until(root, '[data-testid="port-preview"]')
})
export const ReadOnlyWhileYamlIsBroken: S = story(graph(shellOpen), async (root) => {
  await inspect(root, 'build')
  const content = await until(root, '.cm-content')
  content.focus()
  document.execCommand('insertText', false, 'id: [\n')
  await until(root, '[data-testid="inspector-readonly"]')
})
export const Checking: S = story(graph(shellOpen, { validateHang: true }), async (root) => {
  await inspect(root, 'build')
  const content = await until(root, '.cm-content')
  content.focus()
  document.execCommand('insertText', false, '# note\n')
  await until(root, '[data-testid="inspector-checking"]')
})
export const EngineUnreachable: S = story(graph(shellOpen, { validateFails: true }), async (root) => {
  await inspect(root, 'build')
  const content = await until(root, '.cm-content')
  content.focus()
  document.execCommand('insertText', false, '# note\n')
  await wait(700)
})

// Commits text the way a person does: type, then leave the field.
function typeAndLeave(el: HTMLInputElement, text: string) {
  el.focus()
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, text)
  el.dispatchEvent(new Event('input', { bubbles: true }))
  el.blur()
}
const refused = () => editRefused as unknown as { status: number; body: unknown }

// A loop field change sends one targeted operation (set_loop_config), never the
// whole block again; non-integer attempts text is a local error and sends nothing.
const loopSent: { op: string; path?: string[] }[] = []
export const LoopEditsAreTargeted: S = story(
  graph(loopsOpen, { edit: (ops) => (loopSent.push(...ops), refused()) }),
  async (root) => {
    await inspect(root, 'status')
    const select = await until(root, '[data-testid="section-loop"] [data-field-key="operator"] select')
    const sel = select as HTMLSelectElement
    sel.value = [...sel.options].map((o) => o.value).find((v) => v && v !== sel.value)!
    sel.dispatchEvent(new Event('change', { bubbles: true }))
    const end = Date.now() + 4000
    while (loopSent.length === 0 && Date.now() < end) await wait(25)
    const first = loopSent[0]
    if (!first || first.op !== 'set_loop_config' || first.path?.join('.') !== 'operator') throw new Error('a loop field change sent ' + JSON.stringify(loopSent))
    const before = loopSent.length
    typeAndLeave((await until(root, '[data-testid="section-loop"] [data-field-key="max_attempts"] input')) as HTMLInputElement, 'abc')
    await until(root, '[data-testid="field-local-error"]')
    await wait(100)
    if (loopSent.length !== before) throw new Error('invalid max_attempts text was sent: ' + JSON.stringify(loopSent.slice(before)))
  },
)

// A refused port rename puts the file's name back in the field.
export const RefusedRenameRestoresName: S = story(graph(shellOpen, { edit: () => refused() }), async (root) => {
  await inspect(root, 'report')
  const input = (await until(root, '[data-testid="port-name-inputs-text"]')) as HTMLInputElement
  typeAndLeave(input, 'renamed')
  const end = Date.now() + 4000
  while (input.value !== 'text' && Date.now() < end) await wait(25)
  if (input.value !== 'text') throw new Error('the refused name stayed: ' + input.value)
})
