import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import redEvents from './fixtures/standalone-gate-red.json'
import { NodePanel } from '../src/NodePanel'
import { I18nProvider } from '../src/i18n/context'
import { reduceAll } from '../src/reducer'
import type { ApEvent } from '../src/types'

// standalone-gate-red.json: recorded `agentplane run test/fixtures/standalone-gate-red.yaml --json`.
// A Gate node outside a loop emits gate_check without an attempt ID and has no attempts.
const red = redEvents as unknown as ApEvent[]

function panel(locale: 'en' | 'pl') {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <NodePanel run={reduceAll(red)} nodeId="accept" kind="gate" />
    </I18nProvider>,
  )
}

describe('standalone gate check', () => {
  const event = red.find((e) => e.type === 'gate_check')!

  it('is emitted without an attempt ID, for a node with no attempts', () => {
    expect(event.attempt_id).toBeUndefined()
    expect(red.some((e) => e.type === 'attempt_started')).toBe(false)
  })

  it('keeps the verdict, reason and evidence on the node', () => {
    const accept = reduceAll(red).nodes.accept!
    const p = event.payload as Record<string, unknown>
    expect(accept.status).toBe('failed')
    expect(accept.attempts).toHaveLength(0)
    expect(accept.check?.checkId).toBe(p.check_id)
    expect(accept.check?.verdict).toBe('red')
    expect(accept.check?.reason).toBe('predicate mismatch')
    expect(accept.check?.evidence.map((e) => [e.kind, e.source])).toEqual([['json_predicate', 'json_predicate']])
  })

  it('shows the check in node detail instead of the no-loop notice', () => {
    for (const locale of ['en', 'pl'] as const) {
      const html = panel(locale)
      expect(html).toContain('data-testid="check-result"')
      expect(html).toContain('predicate mismatch')
      expect(html).toContain('build-ready')
      expect(html).toContain('data-testid="check-evidence"')
    }
  })

  it('leaves a shell node without a check', () => {
    expect(reduceAll(red).nodes.build!.check).toBeUndefined()
  })
})
