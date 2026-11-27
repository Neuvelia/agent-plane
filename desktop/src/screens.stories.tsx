import type { Meta, StoryObj } from '@storybook/react-vite'
import App from './App'
import { markWelcomeDone } from './capabilities'
import type { BridgeData, BridgeRun, StreamEnding } from './storybook/bridge'
import { agentScopeGraph, asRun, agentScopeIndeterminate, agentScopeRetry, agentScopeYaml, approvalsOf, demoApprove, demoGraph, demoReject, demoYaml, exhaustedGraph, exhaustedRetries, exhaustedYaml, runningParallel, standaloneGateGraph, standaloneGateRed, standaloneGateYaml, summaryOf, waitingForHuman } from './storybook/fixtures'
import { Settings } from './settings'
import { Inbox } from './inboxView'
import { Approvals, RunInspector, RunsList } from './views'
import type { ApEvent } from './types'

const meta: Meta = { title: 'Screens' }

// The whole app as a returning user sees it: the first-run welcome was already left (recorded in
// `beforeEach`, not during render), so it opens on the Inbox.
const returning = () => {
  markWelcomeDone()
}
function ReturningUser() {
  return (
    <div style={{ height: 'calc(100vh - 16px)' }}>
      <App />
    </div>
  )
}
export default meta
type S = StoryObj

function run(events: ApEvent[], status: string, ending: StreamEnding, demo = true): BridgeRun {
  return { summary: summaryOf(events, status), graph: demo ? demoGraph : exhaustedGraph, yaml: demo ? demoYaml : exhaustedYaml, events, ending }
}
const rid = (events: ApEvent[]) => events[0]!.run_id
const noop = () => undefined

const inspector = (events: ApEvent[], status: string, ending: StreamEnding, demo = true): S => ({
  parameters: { bridge: { runs: [run(events, status, ending, demo)] } satisfies BridgeData },
  render: () => <RunInspector runId={rid(events)} onBack={noop} />,
})

export const InspectorRunning: S = inspector(runningParallel, 'running', 'open')
export const InspectorWaitingForHuman: S = inspector(waitingForHuman, 'running', 'open')
export const InspectorSucceeded: S = inspector(demoApprove, 'succeeded', 'end')
const clickTestId = (id: string): S['play'] => async ({ canvasElement }) => {
  await new Promise((r) => setTimeout(r, 100))
  canvasElement.querySelector<HTMLButtonElement>(`[data-testid="${id}"]`)?.click()
  await new Promise((r) => setTimeout(r, 100))
}
export const InspectorYamlView: S = { ...inspector(demoApprove, 'succeeded', 'end'), play: clickTestId('view-yaml') }
export const InspectorSplitView: S = { ...inspector(demoApprove, 'succeeded', 'end'), play: clickTestId('view-split') }
export const InspectorFirstAttempt: S = {
  ...inspector(demoApprove, 'succeeded', 'end'),
  play: async (ctx) => {
    await new Promise((r) => setTimeout(r, 300))
    ctx.canvasElement.querySelector<HTMLButtonElement>('[data-testid="node-flaky"]')?.click()
    await new Promise((r) => setTimeout(r, 100))
    await clickTestId('attempt-tab-1')!(ctx)
  },
}
// F2.8a: compare two attempts of a loop node, from recorded events only.
const openCompare = (node: string): S['play'] => async (ctx) => {
  await new Promise((r) => setTimeout(r, 300))
  ctx.canvasElement.querySelector<HTMLButtonElement>(`[data-testid="node-${node}"]`)?.click()
  await new Promise((r) => setTimeout(r, 100))
  await clickTestId('tab-compare')!(ctx)
}
export const InspectorCompareShellAttempts: S = { ...inspector(demoApprove, 'succeeded', 'end'), play: openCompare('flaky') }
export const InspectorCompareAgentScope: S = {
  parameters: { bridge: { runs: [{ summary: summaryOf(agentScopeRetry, 'succeeded'), graph: agentScopeGraph, yaml: agentScopeYaml, events: agentScopeRetry, ending: 'end' }] } satisfies BridgeData },
  render: () => <RunInspector runId={rid(agentScopeRetry)} onBack={noop} />,
  play: openCompare('work'),
}
// Attempt 2's scope check could not inspect Git (warn mode): shown as undetermined, not clean.
export const InspectorCompareScopeIndeterminate: S = {
  parameters: { bridge: { runs: [{ summary: summaryOf(agentScopeIndeterminate, 'succeeded'), graph: agentScopeGraph, yaml: agentScopeYaml, events: agentScopeIndeterminate, ending: 'end' }] } satisfies BridgeData },
  render: () => <RunInspector runId={rid(agentScopeIndeterminate)} onBack={noop} />,
  play: openCompare('work'),
}
// A standalone Gate has no attempts: its red verdict, reason and evidence show in node detail.
export const InspectorStandaloneGateRed: S = {
  parameters: { bridge: { runs: [{ summary: summaryOf(standaloneGateRed, 'failed'), graph: standaloneGateGraph, yaml: standaloneGateYaml, events: standaloneGateRed, ending: 'end' }] } satisfies BridgeData },
  render: () => <RunInspector runId={rid(standaloneGateRed)} onBack={noop} />,
  play: async (ctx) => {
    await new Promise((r) => setTimeout(r, 300))
    ctx.canvasElement.querySelector<HTMLButtonElement>('[data-testid="node-accept"]')?.click()
    await new Promise((r) => setTimeout(r, 100))
  },
}
export const InspectorRejectedByHuman: S = inspector(demoReject, 'failed', 'end')
// F2.10: the waiting human gate shows its reason and the checks and changed files of the nodes it depends on.
export const InspectorWaitingForHumanContext: S = {
  ...inspector(waitingForHuman, 'running', 'open'),
  play: async (ctx) => {
    await new Promise((r) => setTimeout(r, 300))
    ctx.canvasElement.querySelector<HTMLButtonElement>('[data-testid="node-approve"]')?.click()
    await new Promise((r) => setTimeout(r, 100))
  },
}
export const InspectorFailedAfterRetries: S = inspector(exhaustedRetries, 'failed', 'end', false)
export const InspectorConnectionLost: S = inspector(waitingForHuman, 'running', 'lost')
export const InspectorFailedEvents: S = { ...inspector(demoReject, 'failed', 'end'), play: clickTestId('events-failed') }
// F2.6: node filters, critical path, waits and "what failed", from recorded streams.
export const InspectorFilterRetried: S = { ...inspector(demoApprove, 'succeeded', 'end'), play: clickTestId('nodes-retried') }
export const InspectorFilterHuman: S = { ...inspector(demoApprove, 'succeeded', 'end'), play: clickTestId('nodes-human') }
export const InspectorFilterSlow: S = { ...inspector(demoApprove, 'succeeded', 'end'), play: clickTestId('nodes-slow') }
export const InspectorFilterFailed: S = { ...inspector(demoReject, 'failed', 'end'), play: clickTestId('nodes-failed') }
// Too few finished nodes: Slow is disabled and says why.
export const InspectorSlowUnavailable: S = inspector(runningParallel, 'running', 'open')
export const InspectorRunNotFound: S = {
  parameters: { bridge: { runs: [] } satisfies BridgeData },
  render: () => <RunInspector runId="missing" onBack={noop} />,
}

export const RunsListEmpty: S = { parameters: { bridge: { runs: [] } }, render: () => <RunsList onOpen={noop} /> }
const withSource = (r: BridgeRun, repo_name: string, repo_branch: string | undefined, repo_commit: string): BridgeRun => ({
  ...r,
  summary: { ...r.summary, repo_name, repo_branch, repo_commit, runtime_version: '0.0.0-dev' },
})
export const RunsListBusy: S = {
  parameters: {
    bridge: {
      runs: [
        withSource(run(runningParallel, 'running', 'open'), 'agent-plane', 'main', '1a2b3c4d5e6f7a8b9c0d'),
        withSource(run(demoReject, 'failed', 'end'), 'agent-plane', undefined, '9f8e7d6c5b4a39281706'),
        run(exhaustedRetries, 'failed', 'end', false),
      ],
    } satisfies BridgeData,
  },
  render: () => <RunsList onOpen={noop} />,
}

const pending = approvalsOf(waitingForHuman)
export const ApprovalsEmpty: S = { parameters: { bridge: { runs: [] } }, render: () => <Approvals onOpen={noop} /> }
export const ApprovalsOnePending: S = {
  parameters: { bridge: { runs: [run(waitingForHuman, 'running', 'open')], approvals: pending } satisfies BridgeData },
  render: () => <Approvals onOpen={noop} />,
}
export const ApprovalsManyPending: S = {
  parameters: {
    bridge: { runs: [run(waitingForHuman, 'running', 'open')], approvals: [0, 1, 2].flatMap((i) => pending.map((a) => ({ ...a, node_run_id: `${a.node_run_id}-${i}` }))) } satisfies BridgeData,
  },
  render: () => <Approvals onOpen={noop} />,
}
export const ApprovalsDecisionError: S = {
  parameters: { bridge: { runs: [], approvals: pending, decisionFailure: { status: 500, message: 'request failed (500)' } } satisfies BridgeData },
  render: () => <Approvals onOpen={noop} />,
  play: async ({ canvasElement }) => {
    const approve = [...canvasElement.querySelectorAll('button')].find((b) => b.textContent === 'Approve' || b.textContent === 'Zatwierdź')
    approve?.click()
    await new Promise((r) => setTimeout(r, 50))
  },
}

export const FullAppShell: S = {
  parameters: { bridge: { runs: [run(waitingForHuman, 'running', 'open'), run(exhaustedRetries, 'failed', 'end', false)], approvals: pending } satisfies BridgeData },
  beforeEach: returning,
  render: () => <ReturningUser />,
}

export const SettingsLanguage: S = { parameters: { bridge: { runs: [] } }, render: () => <Settings /> }
// "Record run values" (ADR-0023): off by default, on for this session, and off
// because a configured variable is not set; saving can fail.
export const SettingsRecordingOn: S = { parameters: { bridge: { runs: [], recording: { enabled: true, redactEnv: ['GITHUB_TOKEN'], active: true, missing: [] } } satisfies BridgeData }, render: () => <Settings /> }
export const SettingsRecordingMissingVariable: S = { parameters: { bridge: { runs: [], recording: { enabled: true, redactEnv: ['GITHUB_TOKEN', 'NPM_TOKEN'], active: false, missing: ['NPM_TOKEN'] } } satisfies BridgeData }, render: () => <Settings /> }
export const SettingsRecordingSaveFails: S = { parameters: { bridge: { runs: [], recordingSaveFails: true } satisfies BridgeData }, render: () => <Settings /> }

// Inbox (F2.5b): "now" is pinned just after the recordings so ages and the recent-runs strip are stable.
const afterFixtures = new Date(Math.max(...[demoApprove, demoReject, exhaustedRetries].map((e) => Date.parse(e[0]!.timestamp))) + 3600 * 1000)
const inbox = (b: BridgeData, play?: S['play']): S => ({
  parameters: { bridge: b },
  // The Inbox fills the height it is given, as it fills the app's content area.
  render: () => (
    <div style={{ height: 'calc(100vh - 16px)' }}>
      <Inbox onOpenRun={noop} onNewRun={noop} onOpenGraph={noop} onAllRuns={noop} nowOverride={afterFixtures} />
    </div>
  ),
  play,
})
/** Waits until `selector` matches (the bridge answers asynchronously); a story whose element never appears fails. */
async function waitFor(root: HTMLElement, selector: string, timeoutMs = 3000): Promise<HTMLElement> {
  const end = Date.now() + timeoutMs
  for (;;) {
    const found = root.querySelector<HTMLElement>(selector)
    if (found) return found
    if (Date.now() > end) throw new Error(`story: ${selector} never appeared`)
    await new Promise((r) => setTimeout(r, 25))
  }
}
const clickSelector = (selector: string, then?: string): S['play'] => async ({ canvasElement }) => {
  ;(await waitFor(canvasElement, selector)).click()
  if (then) await waitFor(canvasElement, then)
}
// An interrupted run: the demo recording cut while two branches were running, under its own run ID.
const interrupted = asRun(runningParallel, '7d2e91b4-0c55-4a8e-9f1d-3b6a2c8e5f10')
const triage: BridgeData = {
  runs: [run(waitingForHuman, 'running', 'open'), run(demoReject, 'failed', 'end'), run(exhaustedRetries, 'failed', 'end', false), run(interrupted, 'interrupted', 'end'), run(asRun(runningParallel, 'c41f08a2-9b3d-4e7a-8d65-1f2e3a4b5c6d'), 'running', 'open')],
  approvals: pending,
}
export const InboxEmpty: S = inbox({ runs: [run(demoApprove, 'succeeded', 'end')] })
export const InboxNoRuns: S = inbox({ runs: [] })
export const InboxBusy: S = inbox({ runs: [run(runningParallel, 'running', 'open'), run(demoReject, 'succeeded', 'end'), run(exhaustedRetries, 'succeeded', 'end', false)] })
export const InboxNeedsAttention: S = inbox(triage)
export const InboxFailedSelected: S = inbox(triage, clickSelector('[data-testid="inbox-item"][data-kind="failed"]', '[data-testid="inbox-detail"][data-kind="failed"]'))
export const InboxRetriesUsedUp: S = inbox({ runs: [run(exhaustedRetries, 'failed', 'end', false)] })
export const InboxInterruptedSelected: S = inbox(triage, clickSelector('[data-testid="inbox-item"][data-kind="interrupted"]', '[data-testid="inbox-detail"][data-kind="interrupted"]'))
export const InboxRunningSelected: S = inbox(triage, clickSelector('[data-testid="inbox-item"][data-kind="running"]', '[data-testid="inbox-nodes"]'))
export const InboxFilterStopped: S = inbox(triage, clickSelector('[data-testid="inbox-filter-stopped"]', '[data-testid="inbox-filter-stopped"][aria-pressed="true"]'))
export const InboxFilterEmpty: S = inbox({ runs: [run(demoReject, 'failed', 'end')] }, clickSelector('[data-testid="inbox-filter-approvals"]', '[data-testid="inbox-filter-empty"]'))
// Decided here: the request succeeds, the recorded approval_decided arrives, the list drops the approval; the row stays dimmed with its decision.
export const InboxDecidedHere: S = inbox(
  {
    runs: [{ ...run(waitingForHuman, 'running', 'open'), afterDecision: demoApprove.slice(waitingForHuman.length) }, run(demoReject, 'failed', 'end')],
    approvals: pending,
  },
  clickSelector('[data-testid="inbox-approve"]', '[data-testid="inbox-item"].done'),
)
/** Fails the story (and so the story smoke) when a rendered behaviour does not hold. */
function check(ok: boolean, message: string): void {
  if (!ok) throw new Error(`story: ${message}`)
}
async function until(test: () => boolean, message: string, timeoutMs = 4000): Promise<void> {
  const end = Date.now() + timeoutMs
  while (!test()) {
    if (Date.now() > end) throw new Error(`story: ${message}`)
    await new Promise((r) => setTimeout(r, 25))
  }
}
/** Types as a person would, so React's onChange sees it. */
function typeInto(input: HTMLInputElement, value: string): void {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}
// roadmap: F2.5b-AC6 (rendered): a newer approval arriving while one is shown never swaps the detail.
const otherRun = asRun(waitingForHuman, 'b9d3f1e2-7a6c-4d58-9e21-0c4b8a7f6d13')
const otherApproval = approvalsOf(otherRun).map((a) => ({ ...a, node_run_id: `${a.node_run_id}-other` }))
export const InboxStaysOnShownItem: S = inbox(
  { runs: [run(waitingForHuman, 'running', 'open'), run(otherRun, 'running', 'open')], approvals: pending, laterApprovals: [...otherApproval, ...pending] },
  async ({ canvasElement }) => {
    const input = (await waitFor(canvasElement, '[data-testid="inbox-decide"] input')) as HTMLInputElement
    typeInto(input, 'ship it')
    const rows = () => [...canvasElement.querySelectorAll<HTMLElement>('[data-testid="inbox-item"][data-kind="approval"]')]
    await until(() => rows().length === 2, 'the second approval never arrived')
    check(rows()[0]!.textContent!.includes(otherRun[0]!.run_id.slice(0, 8)), 'the newer approval sorts first')
    const shown = canvasElement.querySelector('[data-testid="inbox-item"][aria-current="true"]')
    check(!!shown?.textContent?.includes(rid(waitingForHuman).slice(0, 8)), 'the approval being read stays selected')
    check(canvasElement.querySelector<HTMLInputElement>('[data-testid="inbox-decide"] input')?.value === 'ship it', 'the typed reason is kept')
  },
)
// roadmap: F2.5b-AC6 (rendered): focus follows the person's own decision only, and is used once.
export const InboxKeyboardFocus: S = inbox(
  { runs: [{ ...run(waitingForHuman, 'running', 'open'), afterDecision: demoApprove.slice(waitingForHuman.length) }, run(demoReject, 'failed', 'end')], approvals: pending },
  async ({ canvasElement }) => {
    ;(await waitFor(canvasElement, '[data-testid="inbox-approve"]')).click()
    const next = await waitFor(canvasElement, '[data-testid="inbox-next"]')
    await until(() => document.activeElement === next, 'focus moves to Next after the decision')
    await until(() => canvasElement.querySelector('[data-testid="inbox-announce"]')!.textContent !== '', 'the decision is announced')
    next.click()
    await waitFor(canvasElement, '[data-testid="inbox-detail"][data-kind="failed"]')
    await until(() => document.activeElement?.tagName === 'H2', 'after Next the new headline takes focus')
    const filter = await waitFor(canvasElement, '[data-testid="inbox-filter-stopped"]')
    filter.focus()
    filter.click()
    await waitFor(canvasElement, '[data-testid="inbox-filter-stopped"][aria-pressed="true"]')
    await new Promise((r) => setTimeout(r, 100))
    check(document.activeElement === filter, 'changing the filter keeps focus on the filter')
    const all = await waitFor(canvasElement, '[data-testid="inbox-filter-all"]')
    all.click()
    const held = await waitFor(canvasElement, '[data-testid="inbox-item"][data-held="true"]')
    held.focus()
    held.click()
    await waitFor(canvasElement, '[data-testid="inbox-decided"]')
    await new Promise((r) => setTimeout(r, 100))
    check(document.activeElement === held, 'opening an item decided earlier keeps focus on its row')
    check(canvasElement.querySelector('[data-testid="inbox-announce"]')!.textContent === '', 'opening it announces nothing')
  },
)
// The listed run cannot be read: the detail says so and deciding is off with the reason.
export const InboxRunNotFound: S = inbox({ runs: [{ ...run(waitingForHuman, 'running', 'open'), unreadable: true }], approvals: pending }, async ({ canvasElement }) => {
  await waitFor(canvasElement, '[data-testid="inbox-run-not-found"]')
})
// The list still shows the approval while the stream already carries the decision: the moment right after deciding.
export const InboxDecided: S = inbox({ runs: [run(demoApprove, 'succeeded', 'end')], approvals: pending })
export const InboxDecisionError: S = inbox({ runs: [run(waitingForHuman, 'running', 'open')], approvals: pending, decisionFailure: { status: 500, message: 'request failed (500)' } }, clickSelector('[data-testid="inbox-approve"]', '[data-testid="inbox-decide"] [role="alert"]'))
export const InboxConnectionLost: S = inbox({ runs: [run(waitingForHuman, 'running', 'lost')], approvals: pending })

// Loading and error states of every screen that reads from the engine.
const hang: BridgeData = { runs: [], hang: true }
const down: BridgeData = { runs: [], unreachable: true }
export const RunsListLoading: S = { parameters: { bridge: hang }, render: () => <RunsList onOpen={noop} /> }
export const RunsListError: S = { parameters: { bridge: down }, render: () => <RunsList onOpen={noop} /> }
export const InboxLoading: S = inbox(hang)
export const InboxError: S = inbox(down)
export const ApprovalsLoading: S = { parameters: { bridge: hang }, render: () => <Approvals onOpen={noop} /> }
export const ApprovalsError: S = { parameters: { bridge: down }, render: () => <Approvals onOpen={noop} /> }
export const FullAppShellEngineDown: S = {
  parameters: { bridge: down },
  beforeEach: returning,
  render: () => <ReturningUser />,
}
