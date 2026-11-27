import type { Meta, StoryObj } from '@storybook/react-vite'
import App from './App'
import { markWelcomeDone } from './capabilities'
import type { BridgeData, BridgeRun, StreamEnding } from './storybook/bridge'
import { agentScopeGraph, asRun, agentScopeIndeterminate, agentScopeRetry, agentScopeYaml, approvalsOf, demoApprove, demoGraph, demoReject, demoYaml, exhaustedGraph, exhaustedRetries, exhaustedYaml, runningParallel, standaloneGateGraph, standaloneGateRed, standaloneGateYaml, summaryOf, waitingForHuman } from './storybook/fixtures'
import { Settings } from './settings'
import { Inbox } from './inboxView'
import { Approvals, RunConfig, RunInspector, RunsList } from './views'
import type { ApEvent } from './types'

const meta: Meta = { title: 'Screens' }

// The whole app as a returning user sees it: the first-run welcome was already left, so it opens on the Inbox.
function ReturningUser() {
  markWelcomeDone()
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

const choose: S['play'] = async ({ canvasElement }) => {
  canvasElement.querySelector<HTMLButtonElement>('[data-testid="choose"]')?.click()
  await new Promise((r) => setTimeout(r, 50))
}
export const NewRunNoFile: S = { parameters: { bridge: { runs: [] } }, render: () => <RunConfig onStarted={noop} /> }
export const NewRunReady: S = {
  parameters: { bridge: { runs: [], pickedGraph: 'demo-vertical.yaml' } satisfies BridgeData },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}
export const NewRunWarnings: S = {
  parameters: {
    bridge: { runs: [], pickedGraph: 'graph.yaml', validate: { status: 'valid', diagnostics: [{ severity: 'warning', code: 'W_UNUSED_OUTPUT', message: 'output "stdout" is never consumed', suggestion: 'bind it to a downstream input or remove it', location: { node_id: 'fast' } }] } } satisfies BridgeData,
  },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}
export const NewRunBlocked: S = {
  parameters: {
    bridge: { runs: [], pickedGraph: 'graph.yaml', validate: { status: 'invalid', diagnostics: [{ severity: 'error', code: 'E_GRAPH_FIELD_REQUIRED', message: 'graph is missing required "edges" collection', suggestion: 'declare the collection, using an empty list when there are no edges' }] } } satisfies BridgeData,
  },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}

export const FullAppShell: S = {
  parameters: { bridge: { runs: [run(waitingForHuman, 'running', 'open'), run(exhaustedRetries, 'failed', 'end', false)], approvals: pending } satisfies BridgeData },
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
const clickSelector = (selector: string): S['play'] => async ({ canvasElement }) => {
  await new Promise((r) => setTimeout(r, 150))
  canvasElement.querySelector<HTMLButtonElement>(selector)?.click()
  await new Promise((r) => setTimeout(r, 150))
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
export const InboxFailedSelected: S = inbox(triage, clickSelector('[data-testid="inbox-item"][data-kind="failed"]'))
export const InboxRetriesUsedUp: S = inbox({ runs: [run(exhaustedRetries, 'failed', 'end', false)] })
export const InboxInterruptedSelected: S = inbox(triage, clickSelector('[data-testid="inbox-item"][data-kind="interrupted"]'))
export const InboxRunningSelected: S = inbox(triage, clickSelector('[data-testid="inbox-item"][data-kind="running"]'))
export const InboxFilterStopped: S = inbox(triage, clickSelector('[data-testid="inbox-filter-stopped"]'))
export const InboxFilterEmpty: S = inbox({ runs: [run(demoReject, 'failed', 'end')] }, clickSelector('[data-testid="inbox-filter-approvals"]'))
// The list still shows the approval while the stream already carries the decision: the moment right after deciding.
export const InboxDecided: S = inbox({ runs: [run(demoApprove, 'succeeded', 'end')], approvals: pending })
export const InboxDecisionError: S = inbox({ runs: [run(waitingForHuman, 'running', 'open')], approvals: pending, decisionFailure: { status: 500, message: 'request failed (500)' } }, clickSelector('[data-testid="inbox-approve"]'))
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
export const NewRunValidating: S = {
  parameters: { bridge: { runs: [], pickedGraph: 'graph.yaml', hang: true } satisfies BridgeData },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}
export const FullAppShellEngineDown: S = {
  parameters: { bridge: down },
  render: () => <ReturningUser />,
}
