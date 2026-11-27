import type { Meta, StoryObj } from '@storybook/react-vite'
import { allAvailable } from '../storybook/capabilityFixtures'
import type { BridgeData } from '../storybook/bridge'
import { AboutDialog, ShortcutsDialog } from './dialogs'
import { ProfileMenu, type ProfileMenuState } from './profileMenu'
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
const footer: NavEntry[] = [
  { id: 'welcome', icon: 'welcome', label: 'nav.welcome' },
  { id: 'settings', icon: 'settings', label: 'nav.settings' },
]
const noop = () => undefined

/** The sidebar as the app draws it, with room on the right for the menu and its submenu (F2.5b). */
function WithProfile({ initial }: { initial: ProfileMenuState }) {
  return (
    <div style={{ width: 520, height: 640, display: 'flex' }}>
      <Sidebar entries={main} deferred={deferred} footer={footer} profile={<ProfileMenu appVersion="0.14.0" initial={initial} onSettings={noop} onShortcuts={noop} onAbout={noop} />} />
    </div>
  )
}

export const SidebarWithDeferredEntries: S = { render: () => <WithProfile initial="closed" /> }
export const Rail: S = { render: () => <div style={{ width: 52, height: 320 }}><IconRail entries={main} /></div> }
export const GlobalBarConnected: S = { render: () => <GlobalBar lost={false} /> }
export const GlobalBarLost: S = { render: () => <GlobalBar lost /> }
export const Breadcrumbs: S = { render: () => <TopBar crumbs={['Runs', '4af0e02b']} /> }

// F2.5b profile menu states, one per design state. The light variants pin the theme.
export const ProfileMenuOpen: S = { render: () => <WithProfile initial="open" /> }
export const ProfileMenuLanguage: S = { render: () => <WithProfile initial="language" /> }
export const ProfileMenuAppearance: S = { render: () => <WithProfile initial="appearance" /> }
export const ProfileMenuAppearanceLight: S = { parameters: { theme: 'light' }, render: () => <WithProfile initial="appearance" /> }
export const ProfileMenuOpenLight: S = { parameters: { theme: 'light' }, render: () => <WithProfile initial="open" /> }

const about: BridgeData = { runs: [], capabilities: { report: allAvailable } }
export const KeyboardShortcuts: S = { render: () => <ShortcutsDialog onClose={noop} /> }
export const AboutTarmind: S = { parameters: { bridge: about }, render: () => <AboutDialog appVersion="0.14.0" onClose={noop} /> }
export const AboutEngineUnreachable: S = { parameters: { bridge: { runs: [], unreachable: true } satisfies BridgeData }, render: () => <AboutDialog appVersion="0.14.0" onClose={noop} /> }
export const AboutTarmindLight: S = { parameters: { bridge: about, theme: 'light' }, render: () => <AboutDialog appVersion="0.14.0" onClose={noop} /> }
