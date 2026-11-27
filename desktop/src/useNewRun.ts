import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { startRun, validateGraph } from './api'
import { getCapabilities } from './capabilities'
import { openDocument, recentGraphs } from './editor/api'
import { NewRunSession, type NewRunPorts } from './newRunSession'
import type { ChosenGraph } from './types'

// F2.5c: binds a NewRunSession to the component's lifetime and to the bridge.

// The bridge is read on each call, so rendering without one (server markup in tests) is safe.
const bridgePorts = (): NewRunPorts => ({
  openGraph: () => window.agentplane.openGraph(),
  openRecentGraph: (id) => window.agentplane.openRecentGraph(id),
  recentGraphs,
  closeGraph: (grant) => window.agentplane.closeGraph(grant),
  validate: validateGraph,
  definition: async (grant) => {
    const r = await openDocument(grant)
    return r.ok ? (r.doc.definition ?? null) : null
  },
  start: startRun,
  capabilities: getCapabilities,
})

export function useNewRun(initialGraph: ChosenGraph | undefined, onStarted: (runId: string) => void) {
  const [session] = useState(() => new NewRunSession(bridgePorts(), initialGraph))
  const state = useSyncExternalStore(session.subscribe, session.snapshot, session.snapshot)
  const mounts = useRef(0)
  // StrictMode unmounts and remounts synchronously, so only a real unmount disposes the session.
  useEffect(() => {
    mounts.current += 1
    session.start()
    // Coming back to the window (for example after saving the file elsewhere) checks the graph again.
    const recheck = () => session.recheck()
    window.addEventListener('focus', recheck)
    return () => {
      mounts.current -= 1
      window.removeEventListener('focus', recheck)
      queueMicrotask(() => {
        if (mounts.current === 0) session.dispose()
      })
    }
  }, [session])
  return {
    ...state,
    setValue: (name: string, value: string) => session.setValue(name, value),
    chooseFile: () => void session.chooseFile(),
    chooseRecent: (r: Parameters<NewRunSession['chooseRecent']>[0]) => void session.chooseRecent(r),
    run: () => void session.run(onStarted),
  }
}
