import type { Meta, StoryObj } from '@storybook/react-vite'
import { GlobalBar, IconRail, Sidebar, TopBar, type NavEntry } from './shell'

const meta: Meta = { title: 'UI/Shell' }
export default meta
type S = StoryObj

const main: NavEntry[] = [
  { id: 'runs', icon: 'runs', label: 'nav.runs', active: true },
  { id: 'approvals', icon: 'approvals', label: 'nav.approvals', badge: 2 },
  { id: 'new', icon: 'new', label: 'nav.new' },
]
const deferred: NavEntry[] = [
  { id: 'projects', icon: 'projects', label: 'nav.projects' },
  { id: 'runners', icon: 'runners', label: 'nav.runners' },
  { id: 'docs', icon: 'docs', label: 'nav.docs' },
]
const footer: NavEntry[] = [{ id: 'settings', icon: 'settings', label: 'nav.settings' }]

export const SidebarWithDeferredEntries: S = { render: () => <div style={{ width: 224, height: 480, display: 'flex' }}><Sidebar entries={main} deferred={deferred} footer={footer} /></div> }
export const Rail: S = { render: () => <div style={{ width: 52, height: 320 }}><IconRail entries={main} /></div> }
export const GlobalBarConnected: S = { render: () => <GlobalBar lost={false} /> }
export const GlobalBarLost: S = { render: () => <GlobalBar lost /> }
export const GlobalBarWithSearch: S = { render: () => <GlobalBar lost={false} search={{ onOpen: () => undefined, keys: 'Ctrl K', ariaKeys: 'Control+K' }} /> }
export const Breadcrumbs: S = { render: () => <TopBar crumbs={['Runs', '4af0e02b']} /> }
