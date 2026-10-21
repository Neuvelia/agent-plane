import { useCallback, useEffect, useRef, useState } from 'react'
import { ChevronDown, CircleHelp, Folder, LayoutDashboard, Monitor, Play, Plus, Settings2, ShieldCheck, Workflow } from 'lucide-react'
import type { CurrentRun, LocalSettings, ProjectView, ToolStatus, WorkspaceView } from '../preload/api'
import { collectPages, updateSetting } from './data'
import { routeAfterProjects, routeFromKey, type Page, type Route } from './navigation'
import { replayRun } from './replay'
import { currentRunView, emptyRunView, type RunView } from './runState'

type Service = 'checking' | 'ready' | 'unavailable'
type LocalData = { projects: ProjectView[]; settings: LocalSettings; tools: ToolStatus[] }
const nav: { page: Page; label: string; icon: typeof LayoutDashboard }[] = [
  { page: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { page: 'project', label: 'Projects', icon: Folder },
  { page: 'runner', label: 'This device', icon: Monitor },
  { page: 'settings', label: 'Settings', icon: Settings2 }
]
function message(error: unknown): string { return error instanceof Error ? error.message : 'Local operation unavailable' }
function Badge({ children, kind = 'quiet' }: { children: React.ReactNode; kind?: 'quiet' | 'good' | 'warn' | 'blue' }): React.JSX.Element {
  return <span className={`badge badge-${kind}`}>{children}</span>
}
function Panel({ title, action, children }: { title: string; action?: React.ReactNode; children: React.ReactNode }): React.JSX.Element {
  return <section className="panel" aria-label={title}><div className="panel-heading"><h2>{title}</h2>{action}</div>{children}</section>
}
function RunSummary({ current, view }: { current: CurrentRun; view: RunView | null }): React.JSX.Element {
  if (current.state === 'none') return <div className="empty-row">No active run on this device</div>
  if (current.state === 'pending') return <div className="empty-row">Start pending · checking with local service</div>
  if (current.state === 'unknown') return <div className="empty-row">Run state unknown · reconnect to verify</div>
  return <div className="data-row run-grid"><span className="mono">{current.run_id?.slice(0, 8)}…</span><span>{view?.events.find((event) => event.type === 'run_started')?.graph_id ?? 'Current local run'}</span><Badge kind={view?.finished ? 'quiet' : 'blue'}>{view?.state ?? 'Checking'}</Badge><span className="muted">{view ? `${view.events.length} retained facts` : 'Loading trace'}</span></div>
}

export function App(): React.JSX.Element {
  const [service, setService] = useState<Service>('checking')
  const [data, setData] = useState<LocalData | null>(null)
  const [dataGeneration, setDataGeneration] = useState(0)
  const [route, setRoute] = useState<Route>({ page: 'dashboard' })
  const [workspaceResult, setWorkspaceResult] = useState<{ projectID: string; items: WorkspaceView[] } | null>(null)
  const [runnerWorkspaces, setRunnerWorkspaces] = useState<WorkspaceView[]>([])
  const [current, setCurrent] = useState<CurrentRun>({ state: 'none' })
  const [runKnown, setRunKnown] = useState(false)
  const [view, setView] = useState<RunView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [graphSelection, setGraphSelection] = useState<{ projectID: string; path: string; opened: string | null } | null>(null)
  const runRef = useRef<RunView | null>(null)
  const serviceEpoch = useRef(0)
  const navRefs = useRef<Array<HTMLButtonElement | null>>([])

  const reload = useCallback(async (): Promise<void> => {
    const api = window.agentplane
    const epoch = ++serviceEpoch.current
    const [projects, settings, git, codex] = await Promise.all([
      collectPages<ProjectView>((limit, cursor) => api.listProjects(limit, cursor), 'projects'),
      api.getSettings(), api.detectTool('git'), api.detectTool('codex')
    ])
    if (epoch !== serviceEpoch.current) return
    setWorkspaceResult(null)
    setRunnerWorkspaces([])
    setData({ projects, settings, tools: [git, codex] })
    setDataGeneration((value) => value + 1)
    setRoute((old) => routeAfterProjects(old, projects))
    setService('ready')
    setError(null)
  }, [])

  useEffect(() => {
    let alive = true
    void reload().catch((cause) => { if (alive) { setService('unavailable'); setError(message(cause)) } })
    const off = window.agentplane.onServiceDisconnected(() => {
      serviceEpoch.current++
      setService('unavailable')
      setError('Local service disconnected. Last shown data may be stale.')
      setRunKnown(false)
      setCurrent((old) => old.state === 'none' ? { state: 'unknown' } : { state: 'unknown', run_id: old.run_id })
    })
    return () => { alive = false; off() }
  }, [reload])

  useEffect(() => {
    if (service !== 'ready') return
    let alive = true
    let inFlight = false
    const refresh = async (): Promise<void> => {
      if (inFlight) return
      inFlight = true
      const epoch = serviceEpoch.current
      try {
        let owned = await window.agentplane.currentRun()
        if (owned.state === 'pending') {
          const result = await window.agentplane.reattachRun()
          if (result) owned = { state: 'known', run_id: result.run_id }
        }
        if (!alive || epoch !== serviceEpoch.current) return
        setCurrent(owned)
        setRunKnown(true)
        if (owned.run_id && owned.state === 'known') {
          const before = runRef.current?.runID === owned.run_id ? runRef.current : emptyRunView(owned.run_id)
          const next = await replayRun(window.agentplane, before)
          if (alive && epoch === serviceEpoch.current) { runRef.current = next; setView(next) }
        } else if (owned.state === 'none' && alive) { runRef.current = null; setView(null) }
      } catch (cause) { if (alive && epoch === serviceEpoch.current) setError(`Run status: ${message(cause)}`) }
      finally { inFlight = false }
    }
    void refresh()
    const timer = window.setInterval(() => { void refresh() }, 1000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [service])

  const selected = route.page === 'project' ? data?.projects.find((item) => item.project_id === route.projectID) : undefined
  const workspacesKnown = !!selected && workspaceResult?.projectID === selected.project_id
  const workspaces = workspacesKnown ? workspaceResult.items : []
  const graphPath = selected && graphSelection?.projectID === selected.project_id ? graphSelection.path : ''
  const openedGraph = selected && graphSelection?.projectID === selected.project_id ? graphSelection.opened : null
  useEffect(() => {
    if (service !== 'ready' || !selected) return
    let alive = true
    void collectPages<WorkspaceView>((limit, cursor) => window.agentplane.listWorkspaces(selected.project_id, limit, cursor), 'workspaces')
      .then((items) => { if (alive) setWorkspaceResult({ projectID: selected.project_id, items }) })
      .catch((cause) => { if (alive) setError(`Workspaces: ${message(cause)}`) })
    return () => { alive = false }
  }, [service, selected?.project_id, dataGeneration])

  useEffect(() => {
    if (service !== 'ready' || route.page !== 'runner' || !data) return
    let alive = true
    void Promise.all(data.projects.map((project) => collectPages<WorkspaceView>((limit, cursor) => window.agentplane.listWorkspaces(project.project_id, limit, cursor), 'workspaces')))
      .then((groups) => { if (alive) setRunnerWorkspaces(groups.flat()) })
      .catch((cause) => { if (alive) setError(`Runner workspaces: ${message(cause)}`) })
    return () => { alive = false }
  }, [service, route.page, data?.projects])

  async function chooseProject(): Promise<void> {
    if (service !== 'ready' || busy) return
    setBusy(true)
    try { const project = await window.agentplane.chooseProjectFolder(); await reload(); if (project) setRoute({ page: 'project', projectID: project.project_id }) }
    catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }
  async function openGraph(): Promise<void> {
    if (!selected || !graphPath.trim() || service !== 'ready') return
    setBusy(true)
    try { const doc = await window.agentplane.openDocument(selected.project_id, graphPath.trim()); setGraphSelection({ projectID: selected.project_id, path: graphPath, opened: `${doc.relative_path} · ${doc.validity} · revision ${doc.revision}` }); setError(null) }
    catch (cause) { setError(message(cause)) }
    finally { setBusy(false) }
  }
  async function saveSetting(key: keyof LocalSettings, value: unknown): Promise<void> {
    if (!data || service !== 'ready' || busy) return
    setBusy(true)
    try { await updateSetting(window.agentplane, data.settings, key, value); await reload() }
    catch (cause) { setError(`${message(cause)}. Reload settings before retrying.`) }
    finally { setBusy(false) }
  }
  function selectPage(page: Page): void {
    if (page === 'project') { const id = selected?.project_id ?? data?.projects[0]?.project_id; setRoute(id ? { page: 'project', projectID: id } : { page: 'dashboard' }) }
    else setRoute({ page })
  }
  const activeView = currentRunView(view, current.run_id)
  const title = route.page === 'project' ? selected?.display_name ?? 'Projects' : route.page === 'runner' ? 'This device' : route.page === 'settings' ? 'Settings' : 'Dashboard'
  return <div className={`shell shell-${route.page}`}>
    <nav className="rail" aria-label="Primary navigation"><div className="brand-mark" aria-label="AgentPlane"><Workflow size={16} strokeWidth={2.2} /></div>
      {nav.map(({ page, label, icon: Icon }, index) => <button key={page} ref={(node) => { navRefs.current[index] = node }} type="button" aria-label={label} aria-current={route.page === page ? 'page' : undefined} className={`rail-button ${route.page === page ? 'active' : ''} ${page === 'settings' ? 'rail-bottom' : ''}`}
        onClick={() => selectPage(page)} onKeyDown={(event) => { const next = routeFromKey(event.key, page); if (next) { event.preventDefault(); const target = nav.findIndex((item) => item.page === next); navRefs.current[target]?.focus(); selectPage(next) } }}><Icon size={16} strokeWidth={1.8} /></button>)}</nav>
    <aside className="sidebar"><div className="sidebar-title"><strong>{route.page === 'dashboard' ? 'Workspace' : route.page === 'runner' ? 'Runner' : title}</strong><CircleHelp size={13} /></div>
      {route.page === 'dashboard' || route.page === 'project' ? <><div className="sidebar-search">⌕ <span>Local projects</span><kbd>⌘K</kbd></div><div className="sidebar-group"><ChevronDown size={11} /> Recent projects</div>
        {data?.projects.length ? data.projects.map((project) => <button type="button" key={project.project_id} className={`sidebar-link ${selected?.project_id === project.project_id ? 'selected' : ''}`} onClick={() => setRoute({ page: 'project', projectID: project.project_id })}><i className="status-dot" />{project.display_name}</button>) : <div className="sidebar-hint">No local projects</div>}
        <div className="sidebar-group"><ChevronDown size={11} /> Graphs</div><div className="sidebar-hint">Open a saved YAML path from a project</div></> : route.page === 'runner' ? <><div className="sidebar-group"><ChevronDown size={11} /> Local runners</div><div className="sidebar-link selected"><i className="status-dot" />This device</div><div className="sidebar-hint">Other devices are not available</div></> : <><div className="sidebar-group"><ChevronDown size={11} /> Application</div><div className="sidebar-link selected">Local settings</div><div className="sidebar-hint">Cloud and policy scopes are unavailable</div></>}
      <div className="sidebar-footer"><span>Local service</span><Badge kind={service === 'ready' ? 'good' : 'warn'}>{service === 'ready' ? 'Connected' : service === 'checking' ? 'Checking' : 'Unavailable'}</Badge></div></aside>
    <div className="main-column"><header className="topbar"><span className="crumb">{route.page === 'project' ? <>Dashboard&nbsp; / &nbsp;Projects&nbsp; / &nbsp;</> : null}<strong>{title}</strong></span><div className="top-actions"><button type="button" className="small-button" disabled={busy} onClick={() => void reload().catch((cause) => setError(message(cause)))}>Refresh</button><Badge>▣ Desktop</Badge><Badge kind={service === 'ready' ? 'good' : 'warn'}>{service === 'ready' ? 'Local only' : 'Stale'}</Badge>{route.page === 'dashboard' && <button type="button" className="small-button" disabled={service !== 'ready' || busy} onClick={() => void chooseProject()}>New project</button>}{(route.page === 'dashboard' || route.page === 'project') && <button type="button" className="small-button primary" disabled title="Graph start opens in the editor in a later feature"><Play size={11} /> Run graph</button>}</div></header>
      <main className={`content content-${route.page}`} aria-live="polite">{service !== 'ready' && <div className="notice" role="status">{service === 'checking' ? 'Connecting to local service…' : 'Local service unavailable. Shown data may be stale.'} <button type="button" onClick={() => void reload().catch((cause) => setError(message(cause)))}>Retry</button></div>}{error && <div className="notice error" role="alert">{error}<button type="button" onClick={() => void reload().catch((cause) => setError(message(cause)))}>Reload local data</button></div>}
        {route.page === 'dashboard' && <><Panel title="Local attention" action={<Badge>—</Badge>}><div className="table-head four"><span>STATUS</span><span>ITEM</span><span>PROJECT</span><span>DETAIL</span></div><div className="data-row four"><Badge>Later</Badge><span>History and approvals become available in later features</span><span>—</span><span>Local only</span></div></Panel><Panel title="Active runs" action={<Badge kind="blue">{!runKnown || current.state === 'pending' || current.state === 'unknown' ? '—' : current.state === 'known' && !activeView?.finished ? '1' : '0'}</Badge>}><div className="table-head run-grid"><span>RUN</span><span>GRAPH</span><span>STATUS</span><span>LATEST EVENT</span></div><RunSummary current={current} view={activeView} /></Panel><div className="quick-actions"><button type="button" disabled={service !== 'ready' || busy} onClick={() => void chooseProject()}><Plus size={18} /><span><strong>New project</strong><small>Choose a local folder</small></span></button><button type="button" disabled><Play size={18} /><span><strong>Run graph</strong><small>Available from the editor</small></span></button><button type="button" disabled><Folder size={18} /><span><strong>Open graph</strong><small>Select a project first</small></span></button><button type="button" disabled><Monitor size={18} /><span><strong>This device only</strong><small>Other runners unavailable</small></span></button></div></>}
        {route.page === 'project' && <><div className="metric-row"><div><small>Graphs</small><strong>—</strong></div><div><small>Project runs</small><strong>—</strong></div><div><small>Workspaces</small><strong>{workspacesKnown ? workspaces.length : '—'}</strong></div><div><small>Project status</small><Badge kind={selected?.availability === 'ready' ? 'good' : 'warn'}>{selected?.availability ?? 'Unknown'}</Badge></div></div><Panel title="Workspaces & targets"><div className="table-head four"><span>RUNNER</span><span>WORKSPACE</span><span>PROJECT</span><span>AVAILABILITY</span></div>{workspacesKnown && workspaces.length ? workspaces.map((workspace) => <div className="data-row four" key={workspace.workspace_id}><span>This device</span><span className="mono">{workspace.relative_path}</span><span>{selected?.display_name}</span><Badge kind={workspace.availability === 'ready' ? 'good' : 'warn'}>{workspace.availability}</Badge></div>) : <div className="empty-row">{workspacesKnown ? 'No registered workspaces' : 'Loading workspaces…'}</div>}</Panel><Panel title="Graphs"><div className="graph-open"><span>Choose a saved YAML path to open. Graph listing arrives with the editor.</span><div><input aria-label="Graph path relative to project" placeholder=".agents/graphs/example.yaml" value={graphPath} onChange={(event) => { if (selected) setGraphSelection({ projectID: selected.project_id, path: event.target.value, opened: null }) }} disabled={service !== 'ready'} /><button type="button" className="small-button" disabled={!graphPath.trim() || service !== 'ready' || busy} onClick={() => void openGraph()}>Open</button></div>{openedGraph && <small>{openedGraph}</small>}</div></Panel><Panel title="Recent runs"><div className="empty-row">Run history is not available yet</div></Panel></>}
        {route.page === 'runner' && <><div className="tabs"><span className="selected">Health</span><span>Capabilities</span><span>Workspaces</span><span>Active runs</span></div><div className="two-col"><Panel title="HEALTH"><div className="kv-row"><span>Service</span><Badge kind={service === 'ready' ? 'good' : 'warn'}>{service === 'ready' ? 'Connected' : 'Unavailable'}</Badge></div>{['Transport', 'Slots', 'Local store', 'Fleet heartbeat'].map((label) => <div className="kv-row" key={label}><span>{label}</span><span className="muted">Unavailable</span></div>)}</Panel><Panel title="CAPABILITIES">{data?.tools.map((tool) => <div className="kv-row" key={tool.tool_id}><span className="mono">{tool.tool_id}</span><Badge kind={tool.status === 'unavailable' ? 'warn' : 'quiet'}>{tool.status === 'unverified' ? 'Configured · unverified' : tool.status}</Badge></div>)}<p className="panel-note">A graph-specific preflight verifies tools before Start.</p></Panel><Panel title="WORKSPACE BINDINGS">{runnerWorkspaces.length ? runnerWorkspaces.map((workspace) => <div className="kv-row" key={workspace.workspace_id}><span><span className="mono">{workspace.relative_path}</span><small> · {data?.projects.find((project) => project.project_id === workspace.project_id)?.display_name ?? 'Unknown project'}</small></span><Badge kind={workspace.availability === 'ready' ? 'good' : 'warn'}>{workspace.availability}</Badge></div>) : <div className="empty-row">No registered local workspaces</div>}</Panel><Panel title="ACTIVE RUNS"><RunSummary current={current} view={activeView} /></Panel></div></>}
        {route.page === 'settings' && <><div className="scope"><span>SCOPE</span><Badge>Application</Badge><span className="muted">Project / Graph / Node scopes unavailable</span></div><Panel title="Effective configuration"><div className="table-head settings-grid"><span>SETTING</span><span>EFFECTIVE</span><span>SOURCE</span><span>STATE</span></div><div className="data-row settings-grid"><span>Theme</span><span>Dark</span><Badge>Application</Badge><span className="muted">Only supported theme</span></div><div className="data-row settings-grid"><span>Max concurrency</span><span>{data?.settings.max_concurrency ?? '—'}</span><Badge>Application</Badge><span>Local setting</span></div><div className="data-row settings-grid"><span>History retention</span><span>{data?.settings.history_retention_days ?? '—'} days</span><Badge>Application</Badge><span className="muted">Stored · applies with F2.9</span></div><div className="data-row settings-grid"><span>Keep all history</span><span>{data?.settings.keep_all_history ? 'On' : 'Off'}</span><Badge>Application</Badge><span className="muted">Stored · applies with F2.9</span></div>{data?.tools.map((tool) => <div className="data-row settings-grid" key={tool.tool_id}><span>{tool.tool_id} executable</span><span className="muted">{tool.path || 'Not configured'}</span><Badge>Application</Badge><span>{tool.status === 'unverified' ? 'Configured · unverified' : tool.status}</span></div>)}</Panel><Panel title="Privacy & local data"><div className="kv-row"><span>Data stays on this device</span><ShieldCheck size={16} /></div><div className="kv-row"><span>Cloud sync</span><span className="muted">Unavailable</span></div></Panel><Panel key={data?.settings.revision} title="Local preferences"><div className="preferences"><label>Max concurrency <input type="number" min="1" max="32" defaultValue={data?.settings.max_concurrency ?? 4} disabled={service !== 'ready' || busy || !data} onBlur={(event) => { const value = Number(event.target.value); if (Number.isInteger(value) && value >= 1 && value <= 32 && value !== data?.settings.max_concurrency) void saveSetting('max_concurrency', value) }} /></label><label>History retention, days <input type="number" min="1" max="3650" defaultValue={data?.settings.history_retention_days ?? 30} disabled={service !== 'ready' || busy || !data || data.settings.keep_all_history} onBlur={(event) => { const value = Number(event.target.value); if (Number.isInteger(value) && value >= 1 && value <= 3650 && value !== data?.settings.history_retention_days) void saveSetting('history_retention_days', value) }} /></label><label>Keep all history <input type="checkbox" checked={data?.settings.keep_all_history ?? false} disabled={service !== 'ready' || busy || !data} onChange={(event) => void saveSetting('keep_all_history', event.target.checked)} /></label><label>Git executable path <input type="text" aria-label="Git executable path" defaultValue={data?.settings.git_binary_path ?? ''} disabled={service !== 'ready' || busy || !data} onBlur={(event) => { const value = event.target.value.trim() || null; if (value !== (data?.settings.git_binary_path ?? null)) void saveSetting('git_binary_path', value) }} /></label><label>Codex executable path <input type="text" aria-label="Codex executable path" defaultValue={data?.settings.codex_binary_path ?? ''} disabled={service !== 'ready' || busy || !data} onBlur={(event) => { const value = event.target.value.trim() || null; if (value !== (data?.settings.codex_binary_path ?? null)) void saveSetting('codex_binary_path', value) }} /></label><small>History preferences are stored now and take effect with F2.9.</small></div></Panel></>}
      </main></div>
  </div>
}
