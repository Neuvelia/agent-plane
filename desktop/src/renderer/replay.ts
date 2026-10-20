import type { AgentPlaneAPI } from '../preload/api'
import { applyPage, applySnapshot, type RunView } from './runState'

type ReplayAPI = Pick<AgentPlaneAPI, 'snapshotRun' | 'eventsRun'>

// A bounded batch keeps the renderer responsive even for a long retained trace.
// The next poll resumes at the last service cursor, including omitted facts.
export async function replayRun(api: ReplayAPI, current: RunView): Promise<RunView> {
  let view = applySnapshot(current, await api.snapshotRun(current.runID))
  for (let pageCount = 0; pageCount < 64; pageCount++) {
    const page = await api.eventsRun(current.runID, view.cursor, 256)
    const previousCursor = view.cursor
    view = applyPage(view, page)
    if (!page.has_more || view.cursor === previousCursor) break
  }
  return applySnapshot(view, await api.snapshotRun(current.runID))
}
