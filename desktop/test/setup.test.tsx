import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CapabilityReport } from '../src/capabilities'
import { I18nProvider } from '../src/i18n/context'
import { BLANK, CLI_COMMAND, rowTone, scanDelay, setupCards, verdictDelay, verdictOf, type SetupStep } from '../src/setup'
import { SUGGESTED, TEMPLATES } from '../src/templates'
import { Welcome } from '../src/welcome'
import recorded from './fixtures/capabilities-linux.json'

const report = recorded as CapabilityReport
const withShell = (status: string): CapabilityReport => ({ ...report, tools: report.tools.map((t) => (t.id === 'shell' ? { ...t, status } : t)) })

describe('setup verdict', () => {
  it('is ready for shell, gate and human graphs when the shell is available, whatever Codex says', () => {
    expect(verdictOf({ state: 'loaded', report: withShell('available'), refreshing: false })).toEqual({ tone: 'ok', key: 'setup.check.ready' })
  })
  it('warns when the shell is missing or unreported', () => {
    expect(verdictOf({ state: 'loaded', report: withShell('missing'), refreshing: false }).tone).toBe('warn')
    expect(verdictOf({ state: 'loaded', report: { ...report, tools: [] }, refreshing: false }).tone).toBe('warn')
  })
  it('is busy while a check runs, even with an older report on screen', () => {
    expect(verdictOf({ state: 'loading' }).tone).toBe('busy')
    expect(verdictOf({ state: 'loaded', report, refreshing: true }).tone).toBe('busy')
  })
  it('is an error when the engine is gone or the check failed', () => {
    expect(verdictOf({ state: 'disconnected' })).toEqual({ tone: 'bad', key: 'setup.check.lost' })
    expect(verdictOf({ state: 'failed', status: 503 })).toEqual({ tone: 'bad', key: 'welcome.failed' })
  })
})

describe('setup row tone', () => {
  it('never treats an unknown status as available', () => {
    expect(rowTone({ id: 'git', kind: 'tool', status: 'brand_new' })).toBe('off')
  })
  it('warns for a missing tool, but not for an agent CLI without adapter or the optional Docker', () => {
    expect(rowTone({ id: 'git', kind: 'tool', status: 'missing' })).toBe('warn')
    expect(rowTone({ id: 'claude', kind: 'agent_cli', status: 'missing' })).toBe('off')
    expect(rowTone({ id: 'docker', kind: 'tool', status: 'missing' })).toBe('off')
    expect(rowTone({ id: 'codex', kind: 'agent_adapter', status: 'error' })).toBe('bad')
  })
})

describe('setup motion', () => {
  it('lands rows in order and the verdict after the last row, in under 3 seconds for the recorded report', () => {
    expect(scanDelay(1).settle).toBeGreaterThan(scanDelay(0).settle)
    const rows = report.tools.length + 1
    expect(verdictDelay(rows)).toBeGreaterThan(scanDelay(rows - 1).settle)
    expect(verdictDelay(rows)).toBeLessThan(3)
  })
  it('turns every animation off under reduced motion', () => {
    const css = readFileSync(join(__dirname, '..', 'src', 'setup.css'), 'utf8')
    expect(css).toMatch(/@media \(prefers-reduced-motion:reduce\)\{\s*\.su \*,\.su \*::before,\.su \*::after\{animation:none !important;transition:none !important\}/)
  })
})

describe('setup template cards', () => {
  it('offer the suggested graph first, one agent graph, one human graph and Blank last', () => {
    const cards = setupCards()
    expect(cards.map((c) => c.id)).toEqual([SUGGESTED, 'dependency-update', 'human-approval', BLANK])
    expect(cards[0]!.recommended).toBe(true)
    expect(cards.filter((c) => c.recommended)).toHaveLength(1)
  })
  it('draw each template from its file, and say when an agent or a value to fill is involved', () => {
    for (const c of setupCards().filter((x) => x.id !== BLANK)) {
      const e = TEMPLATES.find((t) => t.id === c.id)!
      expect(c.flow).toEqual(e.flow)
      expect(c.placeholders).toBe(e.placeholders)
      expect(c.agent).toBe(e.flow.includes('agent'))
    }
    expect(setupCards().find((c) => c.id === BLANK)).toMatchObject({ flow: [], placeholders: 0, agent: false })
  })
  it('skip a template the build does not contain instead of inventing it', () => {
    expect(setupCards([]).map((c) => c.id)).toEqual([BLANK])
  })
})

describe('setup screen', () => {
  afterEach(() => vi.unstubAllGlobals())
  const html = (step: SetupStep, locale: 'en' | 'pl' = 'en') => {
    vi.stubGlobal('window', { agentplane: { appVersion: '1.2.3' } })
    return renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <Welcome onCreate={() => undefined} onOpen={() => undefined} onSkip={() => undefined} initialStep={step} />
      </I18nProvider>,
    )
  }
  it('has one h1, a progress nav whose current segment is marked, and Skip setup in the header', () => {
    const out = html('agents')
    expect(out.match(/<h1/g)).toHaveLength(1)
    expect(out).toMatch(/<nav class="su-prog" aria-label="Setup progress">/)
    expect(out.match(/aria-current="step"/g)).toHaveLength(1)
    expect(out).toContain('aria-label="Step 3: Agent nodes"')
    expect(out).toContain('data-testid="welcome-skip"')
  })
  it('opens on the welcome step with one primary action and no Back', () => {
    const out = html('welcome')
    expect(out).toContain('data-testid="setup-step-welcome"')
    expect(out).not.toContain('data-testid="setup-back"')
    expect(out).toContain('Check this computer')
  })
  it('shows the check step as busy until the report arrives, and never blocks Next', () => {
    const out = html('check')
    expect(out).toContain('aria-busy="true"')
    expect(out).toContain('You can continue without it.')
    expect(out).toMatch(/data-testid="setup-next"(?![^>]*disabled)/)
  })
  it('types the real CLI command on the agents step', () => {
    expect(html('agents')).toContain(CLI_COMMAND.replace(/</g, '&lt;').replace(/>/g, '&gt;'))
  })
  it('preselects the suggested graph and names it in the primary action', () => {
    const out = html('graph')
    expect(out).toMatch(new RegExp(`data-testid="setup-template-${SUGGESTED}"[^>]*checked=""|checked=""[^>]*data-testid="setup-template-${SUGGESTED}"`))
    expect(out).toContain(`Create and open ${SUGGESTED}`)
    expect(out).toContain('data-testid="welcome-open"')
  })
  it('is translated into Polish', () => {
    const out = html('graph', 'pl')
    expect(out).toContain('Wybierz pierwszy graf')
    expect(out).toContain('Pomiń konfigurację')
  })
})
