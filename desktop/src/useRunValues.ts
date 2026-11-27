import { useCallback, useEffect, useMemo, useState } from 'react'
import { listRuns, lookupRun } from './api'
import { usePoll, type PollStatus } from './hooks'
import { classifyEnd } from './reducer'
import type { ApEvent, RunSummary } from './types'
import { initialValues, reduceValues, type RunValues } from './values'
import { choose, confirmVersion, noChoice, pickerRuns, readableRunId, resolveRun, type PickerRun, type ResolvedRun, type RunChoice } from './valuePickerModel'

export type ValuesLoad = 'idle' | 'loading' | 'ready' | 'unavailable'

export interface RunValuesStream {
  load: ValuesLoad
  /** Undefined until a run is chosen, and after the engine said it cannot read the run. */
  values?: RunValues
  /** The stream ended other than normally: what was received may be incomplete. */
  lost: boolean
  /** Subscribes again; values already received stay and replayed events are ignored. */
  reconnect: () => void
}

interface Held {
  runId: string | undefined
  load: ValuesLoad
  values?: RunValues
  lost: boolean
}

// useRunValues replays one run's events and follows it, keeping only values
// (src/values.ts). With no run ID it holds nothing. The state is dropped when the
// run changes and when the component unmounts; it is never written anywhere.
export function useRunValues(runId: string | undefined): RunValuesStream {
  const [held, setHeld] = useState<Held>({ runId: undefined, load: 'idle', lost: false })
  const [attempt, setAttempt] = useState(0)
  const reconnect = useCallback(() => setAttempt((n) => n + 1), [])
  useEffect(() => {
    if (runId === undefined) {
      setHeld({ runId: undefined, load: 'idle', lost: false })
      return undefined
    }
    let alive = true
    // A reconnect keeps what was received; the replay it triggers is idempotent (reduceValues).
    setHeld((h) => (h.runId === runId && h.values ? { ...h, lost: false } : { runId, load: 'loading', values: initialValues(runId), lost: false }))
    // A run the engine cannot read says so up front; its (absent) events would otherwise look like "no values".
    void lookupRun(runId).then((found) => {
      if (alive && found.kind !== 'found') setHeld((h) => (h.runId === runId ? { ...h, load: 'unavailable', values: undefined } : h))
    })
    let pending: ApEvent[] = []
    let timer: ReturnType<typeof setTimeout> | undefined
    const flush = () => {
      timer = undefined
      const batch = pending
      pending = []
      if (batch.length === 0) return
      setHeld((h) => (h.runId === runId && h.values ? { ...h, load: h.load === 'loading' ? 'ready' : h.load, values: batch.reduce(reduceValues, h.values) } : h))
    }
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
        setHeld((h) => (h.runId === runId ? { ...h, load: h.load === 'loading' ? 'ready' : h.load, lost: end.kind === 'lost' } : h))
      },
    )
    return () => {
      alive = false
      stop()
      if (timer) clearTimeout(timer)
    }
  }, [runId, attempt])
  // A state from a previous run must never be shown for the current one.
  return held.runId === runId ? { load: held.load, values: held.values, lost: held.lost, reconnect } : { load: runId === undefined ? 'idle' : 'loading', lost: false, reconnect }
}

export interface ValuesPicker {
  runs: PickerRun[]
  runsStatus: PollStatus
  choice: RunChoice
  resolved: ResolvedRun
  stream: RunValuesStream
  choose: (runId: string | undefined) => void
  confirm: (runId: string) => void
}

// useValuesPicker is "Show values from run" (F2.3 requirement 16) for the open
// document: the runs of its graph ID, the person's choice for as long as the
// caller keeps this hook mounted (key it by document), and the chosen run's values.
export function useValuesPicker(graphId: string | undefined, documentVersion: string | undefined): ValuesPicker {
  const polled = usePoll<RunSummary[]>(listRuns, 3000)
  const [choice, setChoice] = useState<RunChoice>(noChoice)
  const runs = useMemo(() => pickerRuns(polled.data ?? [], graphId, documentVersion), [polled.data, graphId, documentVersion])
  const resolved = useMemo(() => resolveRun(runs, choice), [runs, choice])
  const stream = useRunValues(readableRunId(resolved))
  return {
    runs,
    runsStatus: polled.status,
    choice,
    resolved,
    stream,
    choose: (runId) => setChoice((c) => choose(c, runId)),
    confirm: (runId) => setChoice((c) => confirmVersion(c, runId)),
  }
}
