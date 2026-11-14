import type { Meta, StoryObj } from '@storybook/react-vite'
import App from './App'
import type { BridgeData, BridgeRun, StreamEnding } from './storybook/bridge'
import { approvalsOf, demoApprove, demoGraph, demoReject, demoYaml, exhaustedGraph, exhaustedRetries, exhaustedYaml, runningParallel, summaryOf, waitingForHuman } from './storybook/fixtures'
import { Approvals, Dashboard, RunConfig, RunInspector } from './views'
import type { ApEvent } from './types'

const meta: Meta = { title: 'Screens' }
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
export const InspectorRejectedByHuman: S = inspector(demoReject, 'failed', 'end')
export const InspectorFailedAfterRetries: S = inspector(exhaustedRetries, 'failed', 'end', false)
export const InspectorConnectionLost: S = inspector(waitingForHuman, 'running', 'lost')
export const InspectorRunNotFound: S = {
  parameters: { bridge: { runs: [] } satisfies BridgeData },
  render: () => <RunInspector runId="missing" onBack={noop} />,
}

export const DashboardEmpty: S = { parameters: { bridge: { runs: [] } }, render: () => <Dashboard onOpen={noop} /> }
export const DashboardBusy: S = {
  parameters: {
    bridge: { runs: [run(runningParallel, 'running', 'open'), run(demoReject, 'failed', 'end'), run(exhaustedRetries, 'failed', 'end', false)] } satisfies BridgeData,
  },
  render: () => <Dashboard onOpen={noop} />,
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
  parameters: { bridge: { runs: [], pickedGraph: '/work/.agents/graphs/demo-vertical.yaml' } satisfies BridgeData },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}
export const NewRunWarnings: S = {
  parameters: {
    bridge: { runs: [], pickedGraph: '/work/graph.yaml', validate: { status: 'valid', diagnostics: [{ severity: 'warning', code: 'W_UNUSED_OUTPUT', message: 'output "stdout" is never consumed', suggestion: 'bind it to a downstream input or remove it', location: { node_id: 'fast' } }] } } satisfies BridgeData,
  },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}
export const NewRunBlocked: S = {
  parameters: {
    bridge: { runs: [], pickedGraph: '/work/graph.yaml', validate: { status: 'invalid', diagnostics: [{ severity: 'error', code: 'E_GRAPH_FIELD_REQUIRED', message: 'graph is missing required "edges" collection', suggestion: 'declare the collection, using an empty list when there are no edges' }] } } satisfies BridgeData,
  },
  render: () => <RunConfig onStarted={noop} />,
  play: choose,
}

export const FullAppShell: S = {
  parameters: { bridge: { runs: [run(waitingForHuman, 'running', 'open'), run(exhaustedRetries, 'failed', 'end', false)], approvals: pending } satisfies BridgeData },
  render: () => (
    <div style={{ height: '100vh' }}>
      <App />
    </div>
  ),
}
