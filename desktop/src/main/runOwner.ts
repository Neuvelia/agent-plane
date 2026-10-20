import type { CurrentRun, StartResult, StartSelection } from '../preload/api'
import { SidecarError } from './sidecar'

type SendStart = (selection: StartSelection) => Promise<StartResult>
type OwnedStart = {
  selection: StartSelection
  logical: string
  result?: StartResult
  inFlight?: Promise<StartResult>
  disconnected: boolean
}

function logicalPayload(selection: StartSelection): string {
  const { preflight_token: _token, ...logical } = selection
  return JSON.stringify(logical)
}

// Electron main retains an uncertain Start across renderer reloads. A second
// dispatch always uses the same key and logical payload.
export class RunStartOwner {
  private owned?: OwnedStart

  current(): CurrentRun {
    const owned = this.owned
    if (!owned) return { state: 'none' }
    if (owned.disconnected) return { state: 'unknown', run_id: owned.result?.run_id }
    if (owned.result) return { state: 'known', run_id: owned.result.run_id }
    return { state: 'pending' }
  }

  start(selection: StartSelection, send: SendStart): Promise<StartResult> {
    const copy = structuredClone(selection)
    const logical = logicalPayload(copy)
    if (this.owned) {
      if (this.owned.disconnected) return Promise.reject(new Error('Local service disconnected; run state is unknown'))
      if (this.owned.selection.idempotency_key !== copy.idempotency_key || this.owned.logical !== logical) {
        return Promise.reject(new Error('Another local run or payload is already owned'))
      }
      this.owned.selection.preflight_token = copy.preflight_token
      return this.dispatch(this.owned, send)
    }
    const owned: OwnedStart = { selection: copy, logical, disconnected: false }
    this.owned = owned // Publish before the first sidecar write.
    return this.dispatch(owned, send)
  }

  reattach(send: SendStart): Promise<StartResult | null> {
    if (!this.owned) return Promise.resolve(null)
    if (this.owned.disconnected) return Promise.reject(new Error('Local service disconnected; run state is unknown'))
    return this.dispatch(this.owned, send)
  }

  markDisconnected(): void {
    if (this.owned) this.owned.disconnected = true
  }

  clearAfterConfirmedTerminal(runID: string): void {
    if (this.owned?.result?.run_id === runID) this.owned = undefined
  }

  private dispatch(owned: OwnedStart, send: SendStart): Promise<StartResult> {
    if (owned.result) return Promise.resolve(owned.result)
    if (owned.inFlight) return owned.inFlight
    const pending = send(structuredClone(owned.selection)).then((result) => {
      if (!result || typeof result.run_id !== 'string' || !result.run_id) {
        throw new Error('Local service returned no run identity')
      }
      owned.result = result
      return result
    }).catch((error: unknown) => {
      // A service rejection is definitive. Lost transport or an unvalidated
      // response may have accepted the run, so keep its key for reattachment.
      if (error instanceof SidecarError && !error.uncertain && this.owned === owned) {
        this.owned = undefined
      }
      throw error
    }).finally(() => { owned.inFlight = undefined })
    owned.inFlight = pending
    return pending
  }
}
