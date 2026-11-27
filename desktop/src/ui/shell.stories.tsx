import type { Meta, StoryObj } from '@storybook/react-vite'
import type { ReactNode } from 'react'
import { listApprovals, listRuns } from '../api'
import { usePoll } from '../hooks'
import { RecentRuns } from '../sidebarRecent'
import type { BridgeData } from '../storybook/bridge'
import { approvalsOf, asRun, demoApprove, demoGraph, demoReject, demoYaml, exhaustedRetries, summaryOf, waitingForHuman } from '../storybook/fixtures'
import type { ApEvent } from '../types'
import { GlobalBar, IconRail, Sidebar, TopBar, type NavEntry } from './shell'

const meta: Meta = { title: 'UI/Shell' }
export default meta
type S = StoryObj

const main: NavEntry[] = [
  { id: 'inbox', icon: 'inbox', label: 'nav.inbox', active: true },
  { id: 'runs', icon: 'runs', label: 'nav.runs' },
  { id: 'approvals', icon: 'approvals', label: 'nav.approvals', badge: 2 },
  { id: 'editor', icon: 'editor', label: 'nav.editor' },
]
const primary: NavEntry = { id: 'new', icon: 'new', label: 'nav.new' }
const deferred: NavEntry[] = [
  { id: 'projects', icon: 'projects', label: 'nav.projects' },
  { id: 'runners', icon: 'runners', label: 'nav.runners' },
  { id: 'docs', icon: 'docs', label: 'nav.docs' },
]
const footer: NavEntry[] = [{ id: 'welcome', icon: 'welcome', label: 'nav.welcome' }, { id: 'settings', icon: 'settings', label: 'nav.settings' }]

const summary = (events: ApEvent[], status: string) => ({ summary: summaryOf(events, status), graph: demoGraph, yaml: demoYaml, events, ending: 'end' as const })
const frame = (children: ReactNode) => <div style={{ width: 224, height: 'calc(100vh - 16px)', display: 'flex', flexDirection: 'column' }}>{children}</div>
export const SidebarWithDeferredEntries: S = { render: () => frame(<Sidebar primary={primary} entries={main} deferred={deferred} footer={footer} />) }
// Recent runs come from the run list: newest five, an approval shown as waiting, then "All runs" with the total.
export const SidebarRecentRuns: S = {
  parameters: { bridge: { runs: [summary(waitingForHuman, 'running'), summary(demoReject, 'failed'), summary(exhaustedRetries, 'failed'), summary(asRun(demoApprove, '3b7e1d20-5a4c-4f9e-8c2b-6d1a0e9f7c34'), 'succeeded')], approvals: approvalsOf(waitingForHuman) } satisfies BridgeData },
  render: () => frame(<SidebarWithRecent />),
}
export const SidebarRecentUnavailable: S = { parameters: { bridge: { runs: [], unreachable: true } satisfies BridgeData }, render: () => frame(<SidebarWithRecent />) }
export const SidebarNoRuns: S = { parameters: { bridge: { runs: [] } satisfies BridgeData }, render: () => frame(<SidebarWithRecent />) }

const afterFixtures = new Date(Date.parse(exhaustedRetries[0]!.timestamp) + 3600 * 1000)
function SidebarWithRecent() {
  const runs = usePoll(listRuns, 60_000)
  const approvals = usePoll(listApprovals, 60_000)
  return <Sidebar primary={primary} entries={main} recent={<RecentRuns runs={runs.data} approvals={approvals.data ?? []} failed={runs.status === 'error'} onOpen={() => undefined} onAll={() => undefined} nowOverride={afterFixtures} />} deferred={deferred} footer={footer} />
}
export const Rail: S = { render: () => <div style={{ width: 52, height: 320 }}><IconRail entries={main} /></div> }
export const GlobalBarConnected: S = { render: () => <GlobalBar lost={false} /> }
export const GlobalBarLost: S = { render: () => <GlobalBar lost /> }
export const GlobalBarWithSearch: S = { render: () => <GlobalBar lost={false} search={{ onOpen: () => undefined, keys: 'Ctrl K', ariaKeys: 'Control+K' }} /> }
export const Breadcrumbs: S = { render: () => <TopBar crumbs={['Runs', '4af0e02b']} /> }
