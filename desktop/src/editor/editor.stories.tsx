import type { Meta, StoryObj } from '@storybook/react-vite'
import configErrorOpen from '../../test/fixtures/graph-open-config-error.json'
import anchorsOpen from '../../test/fixtures/graph-open-anchors.json'
import demoOpen from '../../test/fixtures/graph-open-demo.json'
import inputsOpen from '../../test/fixtures/graph-open-with-inputs.json'
import mixedOpen from '../../test/fixtures/graph-open-mixed.json'
import pairOpen from '../../test/fixtures/graph-open-pair.json'
import parseErrorOpen from '../../test/fixtures/graph-open-parse-error.json'
import parseErrorValidate from '../../test/fixtures/graph-validate-parse-error.json'
import parseColumnValidate from '../../test/fixtures/graph-validate-parse-column.json'
import emptyValidate from '../../test/fixtures/graph-validate-empty.json'
import pairValidate from '../../test/fixtures/graph-validate-pair.json'
import editAddNode from '../../test/fixtures/graph-edit-add-node.json'
import editListConnections from '../../test/fixtures/graph-edit-list-connections-demo.json'
import editListBindings from '../../test/fixtures/graph-edit-list-connections-bindings.json'
import editReplace from '../../test/fixtures/graph-edit-replace-bindings.json'
import listPairFrom from '../../test/fixtures/graph-edit-list-connections-pair-from.json'
import listMixedTo from '../../test/fixtures/graph-edit-list-connections-mixed-to.json'
import removeInput from '../../test/fixtures/graph-edit-remove-graph-input-with-inputs.json'
import bindingsOpen from '../../test/fixtures/graph-open-bindings.json'
import bindingsValidate from '../../test/fixtures/graph-validate-bindings.json'
import editDelete from '../../test/fixtures/graph-edit-delete-demo.json'
import editMove from '../../test/fixtures/graph-edit-move.json'
import editRefused from '../../test/fixtures/graph-edit-refused.json'
import editRename from '../../test/fixtures/graph-edit-rename-demo.json'
import fixIssueOpen from '../../test/fixtures/graph-open-fix-issue.json'
import fixIssueValidate from '../../test/fixtures/graph-validate-fix-issue.json'
import fixIssueErrorOpen from '../../test/fixtures/graph-open-fix-issue-error.json'
import fixIssueErrorValidate from '../../test/fixtures/graph-validate-fix-issue-error.json'
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
  return <EditorScreen g={g} onRun={() => undefined} />
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
// Menus are portalled to <body>, so lookups use the whole document.
const click = async (_root: Element, testId: string) => (await until(document.body, `[data-testid="${testId}"]`) as HTMLElement).click()
/** The YAML is on screen in the Split view. */
async function showYaml(root: Element) {
  if (!root.querySelector('.cm-content')) await click(root, 'view-split')
  await until(root, '.cm-content')
}
async function typeIn(root: Element, text: string) {
  await showYaml(root)
  const content = (await until(root, '.cm-content')) as HTMLElement
  content.focus()
  document.execCommand('insertText', false, text)
  await wait(50)
}
const open = async (root: Element) => {
  await click(root, 'editor-open')
  await until(root, '[data-testid="editor-canvas"]')
}
/** Selects a step the keyboard way: Find a step, then its row. */
async function pickNode(root: Element, id: string) {
  await click(root, 'find-step')
  await click(root, `list-node-${id}`)
}
/** Runs an action from a step's "…" menu. */
async function stepAction(root: Element, id: string, action: string) {
  await click(root, `step-menu-${id}`)
  await click(root, action)
}
async function addStep(root: Element, type: string) {
  await click(root, 'add-step')
  await click(root, `add-${type}`)
}
async function openDrawer(root: Element) {
  const toggle = (await until(root, '[data-testid="problems-toggle"]')) as HTMLElement
  if (toggle.getAttribute('aria-expanded') !== 'true') toggle.click()
  await wait(300)
}

const story = (graph: GraphStory, play?: (root: Element) => Promise<void>, theme?: 'light' | 'dark'): S => ({
  parameters: { bridge: { runs: [], graph }, bleed: true, ...(theme ? { theme } : {}) },
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
export const NewGraph: S = story({ ...base, validate: () => emptyValidate as unknown as ValidateResponse }, async (root) => {
  await click(root, 'editor-new')
  await click(root, 'picker-create')
  await until(root, '[data-testid="empty-graph"]')
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
  await click(root, 'graph-menu')
  await click(root, 'editor-open')
  await until(root, '[data-testid="leave-prompt"]')
})
export const OpenFailed: S = story({ ...base, openError: 'the file does not exist' }, async (root) => {
  await click(root, 'editor-open')
  await until(root, '[data-testid="editor-error"]')
})

// ---- the canvas (E2.2): recorded answers of the real engine ---------------------
const demo: GraphStory = {
  ...base,
  opened: demoOpen as unknown as OpenResponse,
  edit: (ops) => {
    const first = ops[0]?.op
    const answers: Record<string, { status: number; body: unknown }> = { list_connections: editListConnections, rename_node: editRename, remove_node: editDelete, add_node: editAddNode, set_layout: editMove }
    return first ? answers[first] : undefined
  },
}
const shown = async (root: Element) => {
  await open(root)
  await until(root, '[data-testid="node-fast"]')
  await wait(300)
}

export const CanvasGraph: S = story(demo, shown)
export const CanvasNodeSelected: S = story(demo, async (root) => {
  await shown(root)
  await pickNode(root, 'approve')
  await until(root, '[data-testid="selected"]')
})
export const CanvasConnectList: S = story(demo, async (root) => {
  await shown(root)
  await stepAction(root, 'fast', 'op-connect')
  await click(root, 'connect-out-stdout')
  await until(root, '[data-testid^="connect-to-"]')
})
export const CanvasRename: S = story(demo, async (root) => {
  await shown(root)
  await stepAction(root, 'fast', 'op-rename')
  await until(root, '[data-testid="rename-form"]')
})
export const CanvasDeleteConfirm: S = story(demo, async (root) => {
  await shown(root)
  await stepAction(root, 'flaky', 'op-delete')
  await until(root, '[data-testid="delete-confirm"]')
})
export const CanvasAddNode: S = story({ ...base, edit: () => editAddNode }, async (root) => {
  await open(root)
  await until(root, '[data-testid="node-a"]')
  await addStep(root, 'shell')
  await until(root, '[data-testid="node-shell_1"]')
})
export const CanvasOperationRefused: S = story({ ...base, edit: () => editRefused }, async (root) => {
  await open(root)
  await until(root, '[data-testid="node-a"]')
  await addStep(root, 'shell')
  await until(root, '[data-testid="editor-error"]')
})
export const CanvasReadOnlyAnchors: S = story({ ...base, opened: anchorsOpen as unknown as OpenResponse }, async (root) => {
  await open(root)
  await until(root, '[data-testid="canvas-readonly"]')
})
export const CanvasYamlBroken: S = story(base, async (root) => {
  await open(root)
  await until(root, '[data-testid="node-a"]')
  await typeIn(root, 'id: [\n')
  await until(root, '[data-testid="canvas-readonly"]')
})
export const CanvasEmptyGraph: S = story({ ...base, validate: () => emptyValidate as unknown as ValidateResponse }, async (root) => {
  await click(root, 'editor-new')
  await click(root, 'picker-create')
  await until(root, '[data-testid="empty-graph"]')
  await wait(400)
})

// ---- Save and run, loading (E2.3) ------------------------------------------------
export const RunInputsRequired: S = story({ ...base, opened: inputsOpen as unknown as OpenResponse }, async (root) => {
  await open(root)
  await until(root, '[data-testid="run-inputs"]')
})
export const RunBlockedByErrors: S = story({ ...base, opened: configErrorOpen as unknown as OpenResponse }, async (root) => {
  await open(root)
  await until(root, '[data-testid="run-blocked"]')
})
export const RunFailedToStart: S = story({ ...base, run: 'error' }, async (root) => {
  await open(root)
  await click(root, 'editor-save-run')
  await until(root, '[data-testid="editor-error"]')
})
export const RunAfterSaveConflict: S = story({ ...base, save: 'conflict' }, async (root) => {
  await open(root)
  await typeIn(root, '# mine\n')
  await wait(500)
  await click(root, 'editor-save-run')
  await until(root, '[data-testid="conflict-banner"]')
})
export const OpeningGraph: S = {
  parameters: { bridge: { runs: [], graph: base, hang: true } },
  render: () => <Harness />,
  play: async ({ canvasElement }) => {
    await click(canvasElement, 'editor-open')
    await until(canvasElement, '[data-testid="editor-open"][disabled]')
  },
}

// ---- the Issues panel (F2.4) -----------------------------------------------------
const mixed: GraphStory = { ...base, opened: mixedOpen as unknown as OpenResponse }
const issueButton = async (root: Element, code: string) => (await until(root, `[data-testid="issue"][data-code="${code}"]`)) as HTMLElement

export const IssuesNone: S = story({ ...base, opened: demoOpen as unknown as OpenResponse }, async (root) => {
  await open(root)
  await openDrawer(root)
  await until(root, '[data-testid="issues-empty"]')
})
export const IssuesWarningsOnly: S = story(base, async (root) => {
  await open(root)
  await openDrawer(root)
  await until(root, '[data-testid="issue"]')
})
export const IssuesErrorsAndWarnings: S = story(mixed, async (root) => {
  await open(root)
  await until(root, '[data-testid="issue"]')
  await wait(300)
})
export const IssuesFiltered: S = story(mixed, async (root) => {
  await open(root)
  await openDrawer(root)
  await click(root, 'issues-filter-warning')
  await until(root, '[data-testid="issue"][data-severity="warning"]')
})
export const IssuesFocusedEdge: S = story(mixed, async (root) => {
  await open(root)
  ;(await issueButton(root, 'E_BINDING_TYPE_MISMATCH')).click()
  await until(root, '[data-testid="selected-heading"]')
  await wait(300)
})
export const IssuesFocusedNode: S = story(mixed, async (root) => {
  await open(root)
  ;(await issueButton(root, 'E_NODE_CONFIG_INVALID')).click()
  await until(root, '[data-testid="selected-heading"]')
  await wait(300)
})
export const IssuesFocusedPort: S = story(mixed, async (root) => {
  await open(root)
  ;(await issueButton(root, 'E_REQUIRED_INPUT_UNBOUND')).click()
  await until(root, '.v2-in.focus, .v2-tok.focus')
})
export const IssuesParseError: S = story(base, async (root) => {
  await open(root)
  await typeIn(root, 'id: [\n')
  await openDrawer(root)
  await until(root, '[data-testid="issue"][data-code="YAML"]')
})
const unknownFieldValidate = (text: string): ValidateResponse => ((text.includes('colour:') ? parseColumnValidate : pairValidate) as unknown as ValidateResponse)
export const IssuesParseErrorColumn: S = story({ ...base, validate: unknownFieldValidate }, async (root) => {
  await open(root)
  await typeIn(root, 'colour: red\n')
  await openDrawer(root)
  const issue = await issueButton(root, 'YAML')
  const position = issue.querySelector('.meta .mono')?.textContent ?? ''
  if (!/\b6\b\D+\b5\b/.test(position)) throw new Error(`the parse issue does not show line 6 and column 5: "${position}"`)
})
export const IssuesValidating: S = story({ ...mixed, validateHang: true }, async (root) => {
  await open(root)
  await typeIn(root, '# note\n')
  await openDrawer(root)
  await until(root, '[data-testid="validating"]')
})
export const IssuesEngineUnreachable: S = story({ ...mixed, validateFails: true }, async (root) => {
  await open(root)
  await typeIn(root, '# note\n')
  await openDrawer(root)
  await until(root, '[data-testid="issues-unreachable"]')
})

// ---- the Bindings panel (F2.3 E5a.2): recorded answers of the real engine -------------
const withBindings: GraphStory = {
  ...base,
  opened: bindingsOpen as unknown as OpenResponse,
  validate: () => bindingsValidate as unknown as ValidateResponse,
  edit: (ops) => (ops[0]?.op === 'list_connections' ? editListBindings : ops[0]?.op === 'remove_edge' ? editReplace : undefined),
}
const panel = async (root: Element) => {
  await open(root)
  await until(root, '[data-testid="binding-row"]')
}
/** Picks a value in a controlled native select the way a person would: set it, then fire change. */
async function choose(root: Element, testId: string, value: string) {
  const select = (await until(root, `[data-testid="${testId}"]`)) as HTMLSelectElement
  Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(select, value)
  select.dispatchEvent(new Event('change', { bubbles: true }))
}

export const BindingsRows: S = story(withBindings, async (root) => {
  await panel(root)
  await click(root, 'binding-select')
})
export const BindingsWithIssues: S = story(mixed, panel)
export const BindingsEmpty: S = story(base, async (root) => {
  await open(root)
  await until(root, '[data-testid="bindings-empty"]')
})
export const BindingsAddForm: S = story(withBindings, async (root) => {
  await panel(root)
  await click(root, 'add-binding-open')
  await choose(root, 'binding-source', 'c.outputs.stdout')
  await until(root, '[data-testid="target-reason"]')
})
export const BindingsReplaceConfirm: S = story(withBindings, async (root) => {
  await panel(root)
  await click(root, 'add-binding-open')
  await choose(root, 'binding-source', 'c.outputs.stdout')
  await click(root, 'replace-b.inputs.text')
  await until(root, '[data-testid="replace-confirm"] [data-testid="replace-apply"]')
})
export const BindingsReadOnly: S = story({ ...base, opened: anchorsOpen as unknown as OpenResponse }, async (root) => {
  await open(root)
  await until(root, '[data-testid="bindings-readonly"]')
})
export const BindingsConflict: S = story({ ...withBindings, save: 'conflict' }, async (root) => {
  await panel(root)
  await typeIn(root, '# mine\n')
  await wait(500)
  await click(root, 'editor-save')
  await until(root, '[data-testid="conflict-banner"]')
})

// ---- graph inputs, graph outputs and hints (F2.3 E5a.3) --------------------------------
const hinted = (opened: unknown): GraphStory => ({
  ...base,
  opened: opened as OpenResponse,
  edit: (ops) => (ops[0]?.op === 'list_connections' ? ('from' in ops[0] ? listPairFrom : listMixedTo) : ops[0]?.op === 'remove_graph_input' ? removeInput : undefined),
})

export const GraphInputsCard: S = story(hinted(inputsOpen), async (root) => {
  await open(root)
  await until(root, '[data-testid="graph-input"]')
})
export const GraphInputRemovalPreview: S = story(hinted(inputsOpen), async (root) => {
  await open(root)
  await click(root, 'remove-graph-input-topic')
  await until(root, '[data-testid="graph-input-preview-apply"]')
})
export const GraphOutputsCard: S = story(hinted(configErrorOpen), async (root) => {
  await open(root)
  await until(root, '[data-testid="graph-output"]')
})
export const HintUnusedOutputOptions: S = story(hinted(pairOpen), async (root) => {
  await open(root)
  const hint = (await until(root, '[data-testid="hint"][data-kind="unused-output"] [data-testid="hint-options"]')) as HTMLElement
  hint.click()
  await until(root, '[data-testid^="hint-option-"]')
})
export const HintUnboundInputOptions: S = story(hinted(mixedOpen), async (root) => {
  await open(root)
  const hint = (await until(root, '[data-testid="hint"][data-ref="b.inputs.free"] [data-testid="hint-options"]')) as HTMLElement
  hint.click()
  await until(root, '[data-testid="hint-candidates"]')
  await wait(200)
})

// ---- Graph editor v2 (F2.5e): the artboards, on the recorded graph they show ----------
const fixIssue: GraphStory = { ...base, opened: fixIssueOpen as unknown as OpenResponse, validate: () => fixIssueValidate as unknown as ValidateResponse, recent: [{ id: 'r1', label: 'fix-issue.yaml' }, { id: 'r2', label: 'code-review.yaml' }] }
const fixIssueError: GraphStory = { ...fixIssue, opened: fixIssueErrorOpen as unknown as OpenResponse, validate: () => fixIssueErrorValidate as unknown as ValidateResponse }
/** The artboards show the agent step selected and the graph unsaved. */
const artboard = async (root: Element) => {
  await open(root)
  await until(root, '[data-testid="node-fix"]')
  await pickNode(root, 'fix')
  await wait(350)
}

export const DesignStepCardsLight: S = story(fixIssue, artboard, 'light')
export const DesignStepCards: S = story(fixIssue, artboard)
export const DesignErrorBlocksRun: S = story(fixIssueError, artboard)
export const DesignGraphMenu: S = story(
  fixIssue,
  async (root) => {
    await artboard(root)
    await click(root, 'graph-menu')
    await until(document.body, '[data-testid="graph-menu-list"]')
    await wait(200)
  },
)
export const DesignStepMenu: S = story(
  fixIssue,
  async (root) => {
    await artboard(root)
    await click(root, 'step-menu-fix')
    await until(document.body, '[data-testid="step-menu"]')
    await wait(200)
  },
)
export const DesignAddStepMenu: S = story(fixIssue, async (root) => {
  await artboard(root)
  await click(root, 'add-step')
  await until(document.body, '[data-testid="add-menu"]')
  await wait(200)
})
export const DesignFindStep: S = story(fixIssue, async (root) => {
  await open(root)
  await click(root, 'find-step')
  await until(document.body, '[data-testid="find-menu"]')
  await wait(200)
})
export const DesignEmptyGraph: S = story(
  { ...base, validate: () => emptyValidate as unknown as ValidateResponse },
  async (root) => {
    await click(root, 'editor-new')
    await click(root, 'picker-create')
    await until(root, '[data-testid="empty-graph"]')
    await wait(400)
  },
)
export const DesignProblemsOpen: S = story(fixIssue, async (root) => {
  await open(root)
  await openDrawer(root)
})
export const DesignSplitView: S = story(fixIssue, async (root) => {
  await artboard(root)
  await showYaml(root)
  await wait(300)
})
export const DesignMinimap: S = story(fixIssue, async (root) => {
  await open(root)
  await click(root, 'minimap-toggle')
  await wait(300)
})
export const DesignEdgeSelected: S = story(fixIssue, async (root) => {
  await open(root)
  await until(root, '.react-flow__edge')
  const edge = root.querySelector('[data-testid="rf__edge-fix.outputs.summary->review.inputs.summary"]') as HTMLElement | null
  edge?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await until(root, '[data-testid="edge-inspector"]')
  await wait(300)
})
