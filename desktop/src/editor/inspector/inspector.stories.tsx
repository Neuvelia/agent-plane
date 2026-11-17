import type { Meta, StoryObj } from '@storybook/react-vite'
import configErrorOpen from '../../../test/fixtures/inspector-open-config-error.json'
import loopsOpen from '../../../test/fixtures/inspector-open-loops.json'
import shellOpen from '../../../test/fixtures/inspector-open-shell.json'
import typesOpen from '../../../test/fixtures/inspector-open-types.json'
import diagnosticsOpen from '../../../test/fixtures/inspector-open-diagnostics.json'
import removePort from '../../../test/fixtures/inspector-edit-remove-port.json'
import renameInput from '../../../test/fixtures/inspector-edit-rename-input.json'
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

async function waitFor(ok: () => boolean, ms = 4000) {
  const end = Date.now() + ms
  while (!ok() && Date.now() < end) await wait(25)
  return ok()
}

// A rename whose preview is cancelled puts the file's name back in the field.
export const CancelledRenameRestoresName: S = story(graph(shellOpen, { edit: () => renameInput as unknown as { status: number; body: unknown } }), async (root) => {
  await inspect(root, 'report')
  const input = (await until(root, '[data-testid="port-name-inputs-text"]')) as HTMLInputElement
  typeAndLeave(input, 'renamed')
  await click(root, 'port-preview-cancel')
  if (!(await waitFor(() => input.value === 'text'))) throw new Error('the cancelled name stayed: ' + input.value)
})

// Attempts text that is not a plain whole number is a local error and sends
// nothing; 0 is sent as 0 (the engine refuses it), never as "unset".
const attemptsSent: { op: string; max_attempts?: number }[] = []
export const InvalidMaxAttempts: S = story(
  graph(loopsOpen, { edit: (ops) => (attemptsSent.push(...ops), refused()) }),
  async (root) => {
    await inspect(root, 'status')
    const input = (await until(root, '[data-testid="section-loop"] [data-field-key="max_attempts"] input')) as HTMLInputElement
    for (const bad of ['abc', '0x10', 'Infinity', '1.5']) {
      typeAndLeave(input, bad)
      await until(root, '[data-testid="section-loop"] [data-field-key="max_attempts"] [data-testid="field-local-error"]')
      await wait(100)
      if (attemptsSent.length > 0) throw new Error(`${bad} was sent: ${JSON.stringify(attemptsSent)}`)
      if (input.getAttribute('aria-invalid') !== 'true') throw new Error(`${bad} is not marked invalid`)
    }
    typeAndLeave(input, '0')
    if (!(await waitFor(() => attemptsSent.length > 0))) throw new Error('0 was not sent')
    const op = attemptsSent[0]!
    if (op.op !== 'set_loop_attempts' || op.max_attempts !== 0) throw new Error('0 was sent as ' + JSON.stringify(op))
  },
)

// A key another row of the map already has is refused visibly; nothing is sent.
const mapSent: unknown[] = []
export const MapKeyCollision: S = story(
  graph(diagnosticsOpen, { edit: (ops) => (mapSent.push(...ops), refused()) }),
  async (root) => {
    await inspect(root, 'status')
    const field = await until(root, '[data-field-key="env"]')
    const key = field.querySelector('input[name="key"]') as HTMLInputElement
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(key, 'MODE')
    key.form!.requestSubmit()
    await until(field, '[data-testid="field-local-error"]')
    await wait(100)
    if (mapSent.length > 0) throw new Error('a taken key was sent: ' + JSON.stringify(mapSent))
  },
)

// A Gate input diagnostic (nodes.ID.loop.gate.inputs.NAME) no field shows is at
// the top of the Loop section; a port's diagnostics describe its controls, and
// its required checkbox is named after the port.
export const GateInputAndPortDiagnostics: S = story(graph(diagnosticsOpen), async (root) => {
  await inspect(root, 'status')
  const want = (path: string) => (diagnosticsOpen as unknown as ValidateResponse).diagnostics.find((d) => d.location?.path === path)!.message
  const top = await until(root, '[data-testid="loop-on"] > [data-testid="inspector-diagnostics"]')
  if (!top.textContent?.includes(want('nodes.status.loop.gate.inputs.spare'))) throw new Error('the gate input diagnostic is not at the top of the Loop section: ' + top.textContent)
  const required = (await until(root, '[data-testid="port-required-inputs-extra"]')) as HTMLInputElement
  if (!required.getAttribute('aria-label')?.includes('extra')) throw new Error('the required checkbox is not named after its port')
  const described = document.getElementById(required.getAttribute('aria-describedby') ?? '')
  if (!described?.textContent?.includes(want('nodes.status.inputs.extra.type'))) throw new Error('the required checkbox is not described by the port diagnostics')
})

// Renaming an output to another name the node type offers also sends that output's type.
const renameSent: { op: string; new_port?: string; type?: string }[] = []
export const OutputRenameCarriesType: S = story(
  graph(shellOpen, { edit: (ops) => (renameSent.push(...ops), refused()) }),
  async (root) => {
    await inspect(root, 'build')
    const select = (await until(root, '[data-testid="port-outputs-stdout"] select')) as HTMLSelectElement
    select.value = 'exit_code'
    select.dispatchEvent(new Event('change', { bubbles: true }))
    if (!(await waitFor(() => renameSent.length > 0))) throw new Error('the rename was not sent')
    const op = renameSent[0]!
    if (op.op !== 'update_port' || op.new_port !== 'exit_code' || op.type !== 'number') throw new Error('the rename sent ' + JSON.stringify(op))
  },
)

// The loop's binding choices are the engine's previewed answers (F2.2 loop and gate
// criterion): the target list offers exactly the targets whose preview the engine
// accepted, a source nothing accepts offers none, and a choice the engine then
// refuses shows its message where the binding was made.
const bindingAsked: { op: string; bindings?: { from: string; to: string }[] }[] = []
const bindingAnswer = (ops: { op: string }[]) => {
  const asked = ops[0] as (typeof bindingAsked)[number]
  bindingAsked.push(asked)
  const added = asked.bindings?.[asked.bindings.length - 1]
  const previews = bindingAsked.filter((a) => a.bindings?.at(-1)?.to === added?.to && a.bindings?.at(-1)?.from === added?.from).length
  if (added?.from !== 'candidate.outputs.stdout' || added.to !== 'gate.inputs.left') return refused()
  return previews <= 2 ? (setCommand as unknown as { status: number; body: unknown }) : refused()
}
export const LoopBindingChoicesAreTheEnginesAnswers: S = story(graph(loopsOpen, { edit: bindingAnswer }), async (root) => {
  await inspect(root, 'diffed')
  const choose = (select: HTMLElement, value: string) => {
    const el = select as HTMLSelectElement
    el.value = value
    el.dispatchEvent(new Event('change', { bubbles: true }))
  }
  const offered = (select: Element | null) => (select ? [...(select as HTMLSelectElement).options].map((o) => o.value).filter(Boolean) : undefined)
  // Every source is previewed against every target; the engine accepts one pair.
  choose(await until(root, '[data-testid="binding-from"]'), 'candidate.outputs.stdout')
  const to = await until(root, '[data-testid="binding-to"]')
  const targets = offered(to)!
  if (targets.join() !== 'gate.inputs.left') throw new Error('the choices are not the engine answers: ' + targets)
  const asked = bindingAsked.filter((a) => a.bindings?.at(-1)?.from === 'candidate.outputs.stdout').map((a) => a.bindings!.at(-1)!.to)
  if (asked.sort().join() !== 'gate.inputs.left,gate.inputs.right') throw new Error('not every target was previewed: ' + asked)
  // A source no preview accepts offers no target.
  choose(await until(root, '[data-testid="binding-from"]'), 'node.inputs.expected')
  if (!(await waitFor(() => offered(document.querySelector('[data-testid="binding-to"]'))?.length === 0))) throw new Error('a refused source still offers targets')
  // The engine accepted the preview and refuses the change itself: its message is shown, nothing is silently dropped.
  choose(await until(root, '[data-testid="binding-from"]'), 'candidate.outputs.stdout')
  await waitFor(() => offered(document.querySelector('[data-testid="binding-to"]'))?.length === 1)
  choose(document.querySelector('[data-testid="binding-to"]') as HTMLElement, 'gate.inputs.left')
  const message = (editRefused as unknown as { body: { error: { message: string } } }).body.error.message
  if (!(await waitFor(() => !!document.querySelector('[data-testid="loop-bindings"] [data-testid="inspector-diagnostics"]')?.textContent?.includes(message)))) throw new Error('the refusal is not shown')
})
