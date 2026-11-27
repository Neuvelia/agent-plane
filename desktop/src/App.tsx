import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { listApprovals, listRuns } from './api'
import { ConnectionProvider, useConnectionReport } from './connection'
import { Inbox } from './inboxView'
import { usePoll } from './hooks'
import { useI18n } from './i18n/context'
import { EditorScreen } from './editor/EditorScreen'
import { useGraphDocument } from './editor/useGraphDocument'
import { Settings } from './settings'
import { GlobalBar, Sidebar, type NavEntry } from './ui/shell'
import { CommandPalette, isMac, usePaletteRuns, type PaletteCommand } from './ui/CommandPalette'
import { runEntries, SHORTCUTS, shortcutText, type GlobalCommand } from './palette'
import { useGlobalShortcuts } from './useShortcuts'
import { runStatusKey } from './status'
import type { ChosenGraph } from './types'
import { Approvals, RunConfig, RunInspector, RunsList } from './views'
import { markWelcomeDone, welcomeDone } from './capabilities'
import { Welcome } from './welcome'
import { RecentRuns } from './sidebarRecent'

type Route = { name: 'inbox' } | { name: 'runs' } | { name: 'approvals' } | { name: 'new'; graph?: ChosenGraph } | { name: 'editor'; picker?: boolean } | { name: 'settings' } | { name: 'run'; id: string } | { name: 'welcome' }

export default function App() {
  // Every screen that cannot reach the engine reports here; the global bar says so while any does.
  const [lostSources, setLostSources] = useState<ReadonlySet<string>>(new Set())
  const report = useCallback((source: string, isLost: boolean) => {
    setLostSources((prev) => {
      if (prev.has(source) === isLost) return prev
      const next = new Set(prev)
      if (isLost) next.add(source)
      else next.delete(source)
      return next
    })
  }, [])
  return (
    <ConnectionProvider report={report}>
      <Shell lost={lostSources.size > 0} />
    </ConnectionProvider>
  )
}

function Shell({ lost }: { lost: boolean }) {
  const { t, locale } = useI18n()
  // First run (F2.11 requirement 10): the welcome screen until one of its actions is chosen.
  const [route, setRoute] = useState<Route>(() => (welcomeDone() ? { name: 'inbox' } : { name: 'welcome' }))
  const polled = usePoll(listApprovals, 1500)
  const polledRuns = usePoll(listRuns, 3000)
  const pending = polled.data?.length ?? 0
  // The open graph lives here so it survives moving to another screen and back.
  const editor = useGraphDocument()
  useConnectionReport('badge', polled.status === 'error')
  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  // After navigating, move focus to the content so keyboard and screen-reader users start there.
  const mainRef = useRef<HTMLElement>(null)
  const first = useRef(true)
  const routeKey = route.name === 'run' ? `run:${route.id}` : route.name === 'new' ? `new:${route.graph?.grant ?? ''}` : route.name === 'editor' && route.picker ? 'editor:picker' : route.name
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    // The welcome screen and the template picker move focus to their own content.
    if (routeKey === 'welcome' || routeKey === 'editor:picker') return
    mainRef.current?.focus({ preventScroll: true })
  }, [routeKey])

  const go = (name: Route['name']) => () => setRoute({ name } as Route)

  // F2.5c command palette and global shortcuts: they only navigate or open what the shell offers.
  const mac = useMemo(isMac, [])
  // The open palette's session: a new number for every opening (it keys the component and the
  // run read), or null when closed.
  const [palette, setPalette] = useState<number | null>(null)
  const sessions = useRef(0)
  const openPalette = () => {
    if (palette === null) setPalette(++sessions.current)
  }
  const paletteRuns = usePaletteRuns(palette)
  const perform: Record<Exclude<GlobalCommand, 'palette'>, () => void> = {
    newRun: go('new'),
    openGraph: () => {
      setRoute({ name: 'editor' })
      editor.open()
    },
    approvals: go('approvals'),
    settings: go('settings'),
    inbox: go('inbox'),
  }
  useGlobalShortcuts(mac, (c) => (c === 'palette' ? openPalette() : perform[c]()))
  const statusText = (s: string) => {
    const key = runStatusKey(s)
    return key ? t(key).toLowerCase() : s
  }
  const commands: PaletteCommand[] = [
    { id: 'new-run', group: 'actions', label: t('nav.new'), hint: '', shortcut: SHORTCUTS.newRun, icon: 'play', perform: perform.newRun },
    { id: 'open-graph', group: 'actions', label: t('palette.openGraph'), hint: '', shortcut: SHORTCUTS.openGraph, icon: 'file', perform: perform.openGraph },
    { id: 'approvals', group: 'actions', label: t('palette.reviewApprovals'), hint: pending ? t('palette.waiting', { count: pending }) : '', shortcut: SHORTCUTS.approvals, icon: 'shield', perform: perform.approvals },
    ...runEntries(paletteRuns.runs, statusText).map((e) => ({ ...e, icon: 'play' as const, perform: () => setRoute({ name: 'run', id: e.runId! }) })),
    { id: 'go-inbox', group: 'goto', label: t('nav.inbox'), hint: '', shortcut: SHORTCUTS.inbox, icon: 'inbox', perform: perform.inbox },
    { id: 'go-runs', group: 'goto', label: t('nav.runs'), hint: '', icon: 'runs', perform: go('runs') },
    { id: 'go-editor', group: 'goto', label: t('nav.editor'), hint: '', icon: 'editor', perform: go('editor') },
    { id: 'go-welcome', group: 'goto', label: t('nav.welcome'), hint: '', icon: 'welcome', perform: go('welcome') },
    { id: 'go-settings', group: 'goto', label: t('nav.settings'), hint: '', shortcut: SHORTCUTS.settings, icon: 'settings', perform: perform.settings },
  ]
  const main: NavEntry[] = [
    { id: 'inbox', icon: 'inbox', label: 'nav.inbox', active: route.name === 'inbox', onSelect: go('inbox') },
    { id: 'runs', icon: 'runs', label: 'nav.runs', active: route.name === 'runs' || route.name === 'run', onSelect: go('runs') },
    { id: 'approvals', icon: 'approvals', label: 'nav.approvals', badge: pending, active: route.name === 'approvals', onSelect: go('approvals') },
    { id: 'editor', icon: 'editor', label: 'nav.editor', active: route.name === 'editor', onSelect: go('editor') },
  ]
  const newRun: NavEntry = { id: 'new', icon: 'new', label: 'nav.new', active: route.name === 'new', onSelect: go('new') }
  const footer: NavEntry[] = [{ id: 'welcome', icon: 'welcome', label: 'nav.welcome', active: route.name === 'welcome', onSelect: go('welcome') }, { id: 'settings', icon: 'settings', label: 'nav.settings', active: route.name === 'settings', onSelect: go('settings') }]
  // Surfaces deferred by F2.5a: greyed out, never navigable.
  const deferred: NavEntry[] = [
    { id: 'projects', icon: 'projects', label: 'nav.projects' },
    { id: 'runners', icon: 'runners', label: 'nav.runners' },
    { id: 'docs', icon: 'docs', label: 'nav.docs' },
  ]
  const leaveWelcome = (next: Route, then?: () => void) => () => {
    markWelcomeDone()
    setRoute(next)
    then?.()
  }
  const crumb = route.name === 'welcome' ? t('nav.welcome') : route.name === 'approvals' ? t('nav.approvals') : route.name === 'new' ? t('nav.new') : route.name === 'settings' ? t('nav.settings') : route.name === 'editor' ? t('nav.editor') : route.name === 'inbox' ? t('nav.inbox') : t('nav.runs')
  return (
    <div className="app">
      <a
        className="skip-link"
        href="#content"
        onClick={(e) => {
          e.preventDefault()
          mainRef.current?.focus()
        }}
      >
        {t('a11y.skip')}
      </a>
      <Sidebar
        primary={newRun}
        entries={main}
        recent={<RecentRuns runs={polledRuns.data} approvals={polled.status === 'error' ? [] : (polled.data ?? [])} failed={polledRuns.status === 'error'} onOpen={(id) => setRoute({ name: 'run', id })} onAll={go('runs')} />}
        deferred={deferred}
        footer={footer}
      />
      <div className="col">
        <GlobalBar
          lost={lost}
          crumbs={[crumb, ...(route.name === 'run' ? [route.id.slice(0, 8)] : [])]}
          search={{ onOpen: openPalette, keys: shortcutText(SHORTCUTS.palette, mac), ariaKeys: mac ? 'Meta+K' : 'Control+K' }}
        />
        <main id="content" ref={mainRef} tabIndex={-1}>
          {route.name === 'inbox' && (
            <Inbox
              onOpenRun={(id) => setRoute({ name: 'run', id })}
              onNewRun={() => setRoute({ name: 'new' })}
              onOpenGraph={(graph) => setRoute({ name: 'new', graph })}
              onAllRuns={go('runs')}
            />
          )}
          {route.name === 'runs' && <RunsList onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'run' && <RunInspector key={route.id} runId={route.id} onBack={go('runs')} />}
          {route.name === 'approvals' && <Approvals onOpen={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'settings' && <Settings />}
          {route.name === 'editor' && <EditorScreen g={editor} startWithPicker={route.picker} onRun={(id) => setRoute({ name: 'run', id })} />}
          {route.name === 'welcome' && <Welcome onTemplate={leaveWelcome({ name: 'editor', picker: true })} onOpen={leaveWelcome({ name: 'editor' }, editor.open)} onSkip={leaveWelcome({ name: 'inbox' })} />}
          {route.name === 'new' && <RunConfig key={route.graph?.grant ?? ''} initialGraph={route.graph} onStarted={(id) => setRoute({ name: 'run', id })} />}
        </main>
      </div>
      {palette !== null && (
        <CommandPalette
          key={palette}
          commands={commands}
          runs={paletteRuns.status}
          mac={mac}
          // Ctrl/Cmd+K during the exit animation: a fresh palette (new key) opens once this one has closed.
          onClose={(reopen) => setPalette(reopen ? ++sessions.current : null)}
        />
      )}
    </div>
  )
}
