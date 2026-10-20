import { useEffect, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import type { CurrentRun } from '../preload/api'
import { replayRun } from './replay'
import { emptyRunView, type RunView } from './runState'
import './style.css'

function App(): React.JSX.Element {
  const [service, setService] = useState<'checking' | 'ready' | 'unavailable'>('checking')
  const [current, setCurrent] = useState<CurrentRun>({ state: 'none' })
  const [runView, setRunView] = useState<RunView | null>(null)
  const [replayError, setReplayError] = useState<string | null>(null)
  const runRef = useRef<RunView | null>(null)

  useEffect(() => {
    let mounted = true
    void Promise.all([window.agentplane.getSettings(), window.agentplane.currentRun()]).then(([, run]) => {
      if (!mounted) return
      setService('ready')
      setCurrent(run)
    }).catch(() => { if (mounted) setService('unavailable') })
    const off = window.agentplane.onServiceDisconnected(() => setService('unavailable'))
    return () => { mounted = false; off() }
  }, [])

  useEffect(() => {
    if (service !== 'ready') return
    let mounted = true
    let busy = false
    const refresh = async (): Promise<void> => {
      if (busy) return
      busy = true
      try {
        let owned = await window.agentplane.currentRun()
        if (owned.state === 'pending') {
          const result = await window.agentplane.reattachRun()
          if (result) owned = { state: 'known', run_id: result.run_id }
        }
        if (!mounted) return
        setCurrent(owned)
        if (owned.run_id) {
          const previous = runRef.current?.runID === owned.run_id ? runRef.current : emptyRunView(owned.run_id)
          const next = await replayRun(window.agentplane, previous)
          if (mounted) { runRef.current = next; setRunView(next); setReplayError(null) }
        }
      } catch (error) {
        if (mounted) setReplayError(error instanceof Error ? error.message : 'Replay unavailable')
      } finally { busy = false }
    }
    void refresh()
    const timer = window.setInterval(() => { void refresh() }, 1000)
    return () => { mounted = false; window.clearInterval(timer) }
  }, [service])

  async function chooseFolder(): Promise<void> {
    try { await window.agentplane.chooseProjectFolder() }
    catch { setService('unavailable') }
  }

  return <main>
    <header><strong>AgentPlane</strong><span>Local desktop</span></header>
    <section aria-live="polite">
      <h1>Local workspace</h1>
      <p>Service: {service}</p>
      <p>Run: {current.state}{current.run_id ? ` · ${current.run_id}` : ''}</p>
      {runView && <p>Trace: {runView.events.length} facts · {runView.state}
        {runView.omitted.retained_omitted_events > 0 ? ` · ${runView.omitted.retained_omitted_events} omitted` : ''}
      </p>}
      {replayError && <p role="alert">Replay: {replayError}</p>}
      {service === 'ready' && <button type="button" onClick={() => void chooseFolder()}>Open project folder</button>}
    </section>
  </main>
}

const root = document.getElementById('root')
if (!root) throw new Error('Renderer root is missing')
createRoot(root).render(<App />)
