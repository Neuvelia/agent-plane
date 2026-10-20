import type { MessageBoxOptions } from 'electron'
import { RunStartOwner } from './runOwner'
import { SidecarClient } from './sidecar'
import type { StartResult } from '../preload/api'

type Prompt = (options: MessageBoxOptions) => Promise<{ response: number }>

export async function confirmQuit(client: SidecarClient | null, runs: RunStartOwner, prompt: Prompt): Promise<boolean> {
  if (!client) {
    if (runs.current().state === 'none') return true
    await prompt({ type: 'error', buttons: ['Keep open'], message: 'Run state is unknown without the local service.' })
    return false
  }
  let current = runs.current()
  if (current.state === 'pending') {
    try { await runs.reattach(async (selection) => client.request('runs.start', selection, 120_000) as Promise<StartResult>) }
    catch { /* An unconfirmed run must keep the app open. */ }
    current = runs.current()
  }
  if (current.state === 'unknown' || current.state === 'pending') {
    await prompt({ type: 'error', buttons: ['Keep open'],
      message: 'Run state is unknown. Reconnect or confirm cleanup before quitting.' })
    return false
  }
  if (current.state === 'known' && current.run_id) {
    let snapshot: Record<string, unknown>
    try { snapshot = await client.request('runs.snapshot', { run_id: current.run_id }) as Record<string, unknown> }
    catch {
      await prompt({ type: 'error', buttons: ['Keep open'], message: 'Run status could not be confirmed.' })
      return false
    }
    if (snapshot.finished !== true) {
      const choice = await prompt({ type: 'warning', buttons: ['Keep open', 'Cancel run and quit'],
        message: 'A local run is still active.' })
      if (choice.response !== 1) return false
      try {
        const cancelled = await client.request('runs.cancel', { run_id: current.run_id }, 120_000) as Record<string, unknown>
        if (!['succeeded', 'failed', 'cancelled'].includes(String(cancelled.state))) {
          await prompt({ type: 'error', buttons: ['Keep open'], message: 'Run cancellation could not be confirmed.' })
          return false
        }
        runs.clearAfterConfirmedTerminal(current.run_id)
      } catch {
        await prompt({ type: 'error', buttons: ['Keep open'], message: 'Run cancellation could not be confirmed.' })
        return false
      }
    }
  }
  try { await client.close(); return true }
  catch {
    await prompt({ type: 'error', buttons: ['Keep open'],
      message: 'Local service cleanup did not finish. The app remains open.' })
    return false
  }
}
