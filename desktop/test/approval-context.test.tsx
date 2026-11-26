import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import approveEvents from './fixtures/demo-approve.json'
import { MAX_CONTEXT_FILES, approvalContext } from '../src/approvalContext'
import { NodePanel } from '../src/NodePanel'
import { I18nProvider } from '../src/i18n/context'
import { reduceAll } from '../src/reducer'
import type { ApEvent } from '../src/types'
import { en } from '../src/i18n/en'
import { pl } from '../src/i18n/pl'

// demo-approve.json: recorded `agentplane run .agents/graphs/demo-vertical.yaml --approve approve --json`.
// "fast" and "flaky" feed the human node "approve"; "flaky" is a Shell loop, red then green.
const recorded = approveEvents as unknown as ApEvent[]
const waiting = recorded.slice(0, recorded.findIndex((e) => e.type === 'approval_requested') + 1)

function panel(events: ApEvent[], locale: 'en' | 'pl', opts: { upstream?: string[]; onOpenNode?: (id: string) => void } = { upstream: ['fast', 'flaky'] }) {
  return renderToStaticMarkup(
    <I18nProvider locale={locale}>
      <NodePanel run={reduceAll(events)} nodeId="approve" kind="human" {...opts} />
    </I18nProvider>,
  )
}

describe('approval context model', () => {
  it('reports the latest check of an upstream loop and leaves a node without one empty', () => {
    const [fast, flaky] = approvalContext(reduceAll(waiting), ['fast', 'flaky', 'fast'])
    expect(approvalContext(reduceAll(waiting), ['fast', 'flaky', 'fast'])).toHaveLength(2)
    expect(fast!.nodeId).toBe('fast')
    expect(fast!.check).toBeUndefined()
    expect(flaky!.check?.verdict).toBe('green')
  })

  it('keeps unobserved changed files unknown instead of an empty list', () => {
    for (const u of approvalContext(reduceAll(waiting), ['fast', 'flaky'])) {
      expect(u.files).toBeUndefined()
      expect(u.moreFiles).toBe(0)
    }
  })

  it('bounds the changed files and counts the rest, from the latest attempt that observed them', () => {
    const paths = Array.from({ length: MAX_CONTEXT_FILES + 3 }, (_, i) => `src/f${String(i).padStart(2, '0')}.go`)
    const evidence = {
      ...waiting.find((e) => e.type === 'attempt_started')!,
      type: 'node_evidence',
      sequence: 1000,
      payload: { changed_files: paths.map((path) => ({ path })) },
    } as unknown as ApEvent
    const [u] = approvalContext(reduceAll([...waiting.slice(0, -1), evidence]), ['flaky'])
    expect(u!.files).toEqual(paths.slice(0, MAX_CONTEXT_FILES))
    expect(u!.moreFiles).toBe(3)
  })

  it('sorts the observed paths before bounding so the listed files are deterministic', () => {
    const paths = Array.from({ length: MAX_CONTEXT_FILES + 2 }, (_, i) => `src/f${String(i).padStart(2, '0')}.go`)
    const shuffled = [...paths].reverse()
    const evidence = {
      ...waiting.find((e) => e.type === 'attempt_started')!,
      type: 'node_evidence',
      sequence: 1000,
      payload: { changed_files: shuffled.map((path) => ({ path })) },
    } as unknown as ApEvent
    const [u] = approvalContext(reduceAll([...waiting.slice(0, -1), evidence]), ['flaky'])
    expect(u!.files).toEqual(paths.slice(0, MAX_CONTEXT_FILES))
    expect(u!.moreFiles).toBe(2)
  })
})

describe('approval context in the panel', () => {
  it('shows the reason, upstream checks and the unobserved-files notice in both languages', () => {
    for (const locale of ['en', 'pl'] as const) {
      const html = panel(waiting, locale)
      expect(html).toContain('data-testid="approval-context"')
      expect(html).toContain((locale === 'en' ? en : pl)['approval.reason'])
      expect(html).toContain((locale === 'en' ? en : pl)['approval.context.noCheck'])
      expect(html).toContain((locale === 'en' ? en : pl)['approval.context.noFiles'])
      expect(html).toContain('data-testid="approval-check"')
    }
  })

  it('offers an open-node action only when the caller can navigate', () => {
    expect(panel(waiting, 'en', { upstream: ['flaky'], onOpenNode: () => undefined })).toContain('Open flaky for attempts and details')
    expect(panel(waiting, 'en', { upstream: ['flaky'] })).not.toContain('Open flaky')
  })

  it('shows no context section without upstream nodes', () => {
    expect(panel(waiting, 'en', {})).not.toContain('data-testid="approval-context"')
  })
})
