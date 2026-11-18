import { useEffect, useMemo, useState } from 'react'
import { getRun, lookupRun } from './api'
import { watchLiveElsewhere } from './liveElsewhere'
import { classifyEnd, initialRun, presentRun, reduce, streamLost, type RunView } from './reducer'
import type { ApEvent, GraphSummary } from './types'

export type PollStatus = 'loading' | 'ready' | 'error'

export interface Polled<T> {
  /** The last successful read. Undefined until the first one. */
  data: T | undefined
  /** loading: no answer yet. error: the latest read failed, so `data` may be stale and must not be shown as current. */
  status: PollStatus
  retry: () => void
}

// usePoll re-reads a list on an interval. It is for lists (runs, approvals),
// never for node state, which comes only from the event stream. A failed read is
// reported (status 'error'), not swallowed, so screens can say the engine is unreachable.
export function usePoll<T>(read: () => Promise<T>, ms = 1500): Polled<T> {
  const [state, setState] = useState<{ data: T | undefined; status: PollStatus }>({ data: undefined, status: 'loading' })
  const [nonce, setNonce] = useState(0)
  useEffect(() => {
    let alive = true
    const tick = () =>
      read().then(
        (v) => alive && setState({ data: v, status: 'ready' }),
        () => alive && setState((p) => ({ data: p.data, status: 'error' })),
      )
    tick()
    const t = setInterval(tick, ms)
    return () => {
      alive = false
      clearInterval(t)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ms, nonce])
  return { ...state, retry: () => setNonce((n) => n + 1) }
}

export interface RunStream {
  run: RunView
  graph: GraphSummary | null
  yaml: string
  following: boolean
  notFound: boolean
  /** The run was written by a newer engine than this one can read. */
  unsupported: boolean
  /** Set (to a StreamEnd code) when the stream ended for any reason other than the run's normal end. */
  lost: string | null
  reconnect: () => void
}

// useRunStream replays a run's events from sequence zero and follows it. The
// returned RunView is a pure reduction of those events (src/reducer.ts).
export function useRunStream(runId: string): RunStream {
  const [run, setRun] = useState<RunView>(initialRun())
  const [ended, setEnded] = useState(false)
  const [graph, setGraph] = useState<GraphSummary | null>(null)
  const [yaml, setYaml] = useState('')
  const [notFound, setNotFound] = useState(false)
  const [unsupported, setUnsupported] = useState(false)
  const [storeStatus, setStoreStatus] = useState<string | undefined>(undefined)
  const [lost, setLost] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  // The inspector is keyed by runId, so state starts fresh per run. A reconnect
  // re-subscribes without clearing it: replayed events are ignored by sequence.
  useEffect(() => {
    setEnded(false)
    setLost(null)
    setNotFound(false)
    setUnsupported(false)
    setStoreStatus(undefined)
    let pending: ApEvent[] = []
    let timer: ReturnType<typeof setTimeout> | undefined
    // Coalesce bursts into one render; every event is still applied in order.
    const flush = () => {
      timer = undefined
      const batch = pending
      pending = []
      if (batch.length) setRun((r) => batch.reduce(reduce, r))
    }
    lookupRun(runId).then((d) => {
      if (d.kind === 'unsupported') return setUnsupported(true)
      if (d.kind === 'missing') return setNotFound(true)
      setGraph(d.summary)
      setYaml(d.graph)
      setStoreStatus(d.run.status)
    })
    const stop = window.agentplane.subscribe(
      runId,
      (e) => {
        pending.push(e)
        timer ??= setTimeout(flush, 30)
      },
      (reason) => {
        if (timer) clearTimeout(timer)
        flush()
        const end = classifyEnd(reason)
        if (end.kind === 'lost') setLost(end.code)
        else setEnded(true)
      },
    )
    return () => {
      stop()
      if (timer) clearTimeout(timer)
    }
  }, [runId, attempt])

  // A run the store says is live in another process: when its writer finishes or
  // dies, replay the stored events by subscribing again (replays are idempotent).
  useEffect(() => {
    if (!ended || storeStatus !== 'running') return undefined
    return watchLiveElsewhere(
      () => getRun(runId).then((d) => d?.run.status ?? null),
      () => setAttempt((n) => n + 1),
    )
  }, [ended, storeStatus, runId])

  // Memoized so an unchanged stream keeps the same object and downstream memos hold.
  // Until the store has said whether another process still holds the run, a log
  // without a terminal event is not settled: it would flash "interrupted".
  const shown = useMemo(() => (storeStatus === undefined ? run : presentRun(run, ended, lost !== null, storeStatus)), [run, ended, lost, storeStatus])
  const activeLost = streamLost(shown, lost)
  return { run: shown, graph, yaml, following: !ended && !activeLost, notFound, unsupported, lost: activeLost, reconnect: () => setAttempt((n) => n + 1) }
}
