import { useEffect, useState } from 'react'
import { getRun } from './api'
import { initialRun, reduce, settle, type RunView } from './reducer'
import type { ApEvent, GraphSummary } from './types'

// usePoll re-reads a list on an interval. It is for lists (runs, approvals),
// never for node state, which comes only from the event stream.
export function usePoll<T>(read: () => Promise<T>, initial: T, ms = 1500): T {
  const [value, setValue] = useState<T>(initial)
  useEffect(() => {
    let alive = true
    const tick = () =>
      read()
        .then((v) => alive && setValue(v))
        .catch(() => undefined)
    tick()
    const t = setInterval(tick, ms)
    return () => {
      alive = false
      clearInterval(t)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ms])
  return value
}

export interface RunStream {
  run: RunView
  graph: GraphSummary | null
  yaml: string
  following: boolean
  notFound: boolean
}

// useRunStream replays a run's events from sequence zero and follows it. The
// returned RunView is a pure reduction of those events (src/reducer.ts).
export function useRunStream(runId: string): RunStream {
  const [run, setRun] = useState<RunView>(initialRun())
  const [ended, setEnded] = useState(false)
  const [graph, setGraph] = useState<GraphSummary | null>(null)
  const [yaml, setYaml] = useState('')
  const [notFound, setNotFound] = useState(false)

  useEffect(() => {
    setRun(initialRun())
    setEnded(false)
    setGraph(null)
    setNotFound(false)
    let pending: ApEvent[] = []
    let timer: ReturnType<typeof setTimeout> | undefined
    // Coalesce bursts into one render; every event is still applied in order.
    const flush = () => {
      timer = undefined
      const batch = pending
      pending = []
      if (batch.length) setRun((r) => batch.reduce(reduce, r))
    }
    getRun(runId).then((d) => {
      if (!d) return setNotFound(true)
      setGraph(d.summary)
      setYaml(d.graph)
    })
    const stop = window.agentplane.subscribe(
      runId,
      (e) => {
        pending.push(e)
        timer ??= setTimeout(flush, 30)
      },
      () => {
        if (timer) clearTimeout(timer)
        flush()
        setEnded(true)
      },
    )
    return () => {
      stop()
      if (timer) clearTimeout(timer)
    }
  }, [runId])

  return { run: settle(run, !ended), graph, yaml, following: !ended, notFound }
}
