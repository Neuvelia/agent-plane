// @vitest-environment jsdom
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it } from 'vitest'
import { Inbox } from '../src/inboxView'
import { I18nProvider } from '../src/i18n/context'
import type { RunSummary } from '../src/types'

// The Inbox keeps the shown item in `picked`. An effect pins the first item once the lists
// arrive; a click that lands before that effect runs must not be overwritten by it.
// React is left outside act() so the commit and its passive effects are separate tasks.
declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean
}

const summary = (id: string, status: string): RunSummary => ({ run_id: id, graph_id: `graph-${id}`, started_at: '2026-11-28T10:00:00Z', status, event_count: 1 })

afterEach(() => {
  document.body.innerHTML = ''
})

describe('Inbox selection', () => {
  // roadmap: F2.5b-AC6
  it('[F2.5b-AC6] keeps an item clicked in the same tick as the first render', async () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = false
    const runs = [summary('failed1', 'failed'), summary('running1', 'running')]
    Object.assign(window, {
      agentplane: {
        request: async (_method: string, path: string) => (path === '/runs' ? { status: 200, body: { runs } } : path === '/approvals' ? { status: 200, body: { approvals: [] } } : { status: 404, body: null }),
        subscribe: () => () => undefined,
      },
    })
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    // A mutation observer callback is a microtask: it runs right after the commit task, before React's passive effects.
    const clicked = new Promise<void>((resolve) => {
      const watch = new MutationObserver(() => {
        const row = host.querySelector<HTMLElement>('[data-testid="inbox-item"][data-kind="running"]')
        if (!row) return
        watch.disconnect()
        row.click()
        resolve()
      })
      watch.observe(host, { childList: true, subtree: true })
    })
    root.render(
      <I18nProvider locale="en">
        <Inbox onOpenRun={() => undefined} onNewRun={() => undefined} onOpenGraph={() => undefined} onAllRuns={() => undefined} />
      </I18nProvider>,
    )
    await clicked
    await new Promise((r) => setTimeout(r, 50))
    const selected = host.querySelector('[data-testid="inbox-item"][aria-current="true"]')
    expect(selected?.getAttribute('data-kind')).toBe('running')
    root.unmount()
  })
})
