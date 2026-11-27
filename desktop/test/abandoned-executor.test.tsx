import type { ReactElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import { NodePanel } from '../src/NodePanel'
import { OutcomePill } from '../src/status'
import { I18nProvider } from '../src/i18n/context'
import { reduceAll } from '../src/reducer'
import type { ApEvent } from '../src/types'
import { en } from '../src/i18n/en'
import { pl } from '../src/i18n/pl'

// demo-approve.json: recorded `agentplane run .agents/graphs/demo-vertical.yaml --approve approve --json`.
const approve = approveEvents as unknown as ApEvent[]
const started = approve.find((e) => e.type === 'node_started' && e.node_id === 'fast')!
const abandoned: ApEvent = { ...started, sequence: 1000, type: 'executor_abandoned', payload: { reason: 'node_timeout', bound_ms: 7000 } }

function render(node: ReactElement, locale: 'en' | 'pl' = 'en') {
  return renderToStaticMarkup(<I18nProvider locale={locale}>{node}</I18nProvider>)
}

describe('abandoned executor and timed-out attempts in the desktop (ADR-0044)', () => {
  it('says in node detail that an abandoned executor may still be running, in both languages', () => {
    for (const locale of ['en', 'pl'] as const) {
      const html = render(<NodePanel run={reduceAll([...approve, abandoned])} nodeId="fast" kind="shell" />, locale)
      const text = (locale === 'en' ? en : pl)['node.abandoned'].replace('{reason}', 'node_timeout')
      expect(html).toContain('data-testid="abandoned"')
      expect(html).toContain(text)
    }
  })

  it('shows no notice for a node whose executor returned', () => {
    expect(render(<NodePanel run={reduceAll(approve)} nodeId="fast" kind="shell" />)).not.toContain('data-testid="abandoned"')
  })

  it('shows a timed-out attempt as a retriable warning like a rejected one, not as an error', () => {
    expect(render(<OutcomePill outcome="timeout" />)).toContain('t-w')
    expect(render(<OutcomePill outcome="rejected" />)).toContain('t-w')
    expect(render(<OutcomePill outcome="execution_error" />)).toContain('t-x')
  })
})
