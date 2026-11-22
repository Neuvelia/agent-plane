/** How often the status of a run live in another process is re-read. */
export const LIVE_ELSEWHERE_POLL_MS = 3000

/**
 * A run the store reports as `running` while this window follows no writer is
 * owned by another process. The event stream replays what is stored and ends, so
 * nothing would tell the window that the writer finished or died. This re-reads
 * only the run's status (node state stays a pure function of events, never of a
 * poll) and calls `onChanged` once when it is no longer `running`; the caller
 * then replays the stored events. A read that fails or finds no run is retried on
 * the next tick. Returns a function that stops the watch.
 */
export function watchLiveElsewhere(readStatus: () => Promise<string | null>, onChanged: () => void, ms = LIVE_ELSEWHERE_POLL_MS): () => void {
  let alive = true
  let reading = false
  const timer = setInterval(() => {
    if (reading) return
    reading = true
    readStatus().then(
      (status) => {
        reading = false
        if (!alive || status === null || status === 'running') return
        alive = false
        clearInterval(timer)
        onChanged()
      },
      () => {
        reading = false
      },
    )
  }, ms)
  return () => {
    alive = false
    clearInterval(timer)
  }
}
