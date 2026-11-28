import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CapabilityReport } from '../src/capabilities'
import { I18nProvider } from '../src/i18n/context'
import { BLANK, CLI_COMMAND, rowTone, scanDelay, setupCards, setupDraft, verdictDelay, verdictOf, type SetupStep } from '../src/setup'
import { createOutcome } from '../src/setupCreate'
import { windows } from '../src/storybook/capabilityFixtures'
import { SUGGESTED, TEMPLATES, templateDraft } from '../src/templates'
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
    expect(setupCards(null, []).map((c) => c.id)).toEqual([BLANK])
  })
})

describe('setup create action', () => {
  it('creates Blank with no draft maker, so the editor uses its empty graph', () => {
    expect(setupDraft(BLANK)).toEqual({ usable: true, make: undefined })
  })
  it('creates a template as that template with only its id line changed', () => {
    for (const id of ['dependency-update', SUGGESTED]) {
      const d = setupDraft(id)
      const entry = TEMPLATES.find((t) => t.id === id)!
      expect(d.usable, id).toBe(true)
      if (!d.usable || !d.make) throw new Error(`${id}: no draft maker`)
      expect(d.make('picked.yaml'), id).toBe(templateDraft(entry, 'picked.yaml'))
      expect(d.make('picked.yaml'), id).toContain('id: picked')
    }
  })
  it('refuses a template that is missing or cannot take a new id, instead of creating another graph', () => {
    expect(setupDraft('not-bundled')).toEqual({ usable: false })
    const broken = { ...TEMPLATES[0]!, text: 'schema_version: 0\n' }
    expect(setupDraft(broken.id, [broken])).toEqual({ usable: false })
  })
})

describe('setup create outcome', () => {
  const start = { grant: 'g-old', error: null }
  it('completes setup only when a new document is open', () => {
    expect(createOutcome(start, { grant: 'g-new', error: null, leave: false })).toBe('created')
    expect(createOutcome({ grant: null, error: null }, { grant: 'g-new', error: null, leave: false })).toBe('created')
  })
  it('waits on setup while the save dialog is open or after it was cancelled', () => {
    expect(createOutcome(start, { grant: 'g-old', error: null, leave: false })).toBe('pending')
    expect(createOutcome({ grant: null, error: null }, { grant: null, error: null, leave: false })).toBe('pending')
  })
  it('shows the editor for its unsaved-text prompt, and for a new error but not an old one', () => {
    expect(createOutcome(start, { grant: 'g-old', error: null, leave: true })).toBe('ask')
    const old = { kind: 'open', message: 'earlier' }
    expect(createOutcome({ grant: null, error: old }, { grant: null, error: old, leave: false })).toBe('pending')
    expect(createOutcome({ grant: null, error: old }, { grant: null, error: { kind: 'open', message: 'denied' }, leave: false })).toBe('failed')
  })
})

describe('setup template notices (F2.12 requirement 7)', () => {
  it('without a report, says nothing about the platform and marks tools not checked', () => {
    const cards = setupCards(null)
    expect(cards.every((c) => c.platformOk)).toBe(true)
    expect(cards.find((c) => c.id === 'dependency-update')!.needs.map((n) => [n.id, n.checked])).toEqual([['git', false], ['codex', false]])
  })
  it('on a system the commands do not support, drops Recommended and says so', () => {
    const cards = setupCards(windows)
    const suggested = cards.find((c) => c.id === SUGGESTED)!
    expect(suggested.platformOk).toBe(false)
    expect(suggested.recommended).toBe(false)
    expect(cards.find((c) => c.id === BLANK)!.platformOk).toBe(true)
  })
  it('reports each tool a template needs with the status the engine gave', () => {
    const needs = setupCards(windows).find((c) => c.id === 'dependency-update')!.needs
    expect(needs.map((n) => [n.id, n.tool?.status])).toEqual([['git', 'unsupported_platform'], ['codex', 'unsupported_platform']])
  })
})

describe('setup screen', () => {
  afterEach(() => vi.unstubAllGlobals())
  const html = (step: SetupStep, locale: 'en' | 'pl' = 'en', lost = false) => {
    vi.stubGlobal('window', { agentplane: { appVersion: '1.2.3' } })
    return renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <Welcome onCreate={() => undefined} onOpen={() => undefined} onSkip={() => undefined} initialStep={step} lost={lost} />
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
  it('says in its header when the engine connection is lost, on every step', () => {
    for (const step of ['welcome', 'check', 'agents', 'graph'] as const) {
      expect(html(step, 'en', true), step).toMatch(/data-testid="setup-connection-lost"[^>]*>.*Connection lost/)
      expect(html(step), step).not.toContain('setup-connection-lost')
    }
  })
  it('names the Blank card in words, while template cards keep their file ids', () => {
    const en = html('graph')
    expect(en).toMatch(/<span class="nm word">Blank graph<\/span>/)
    expect(en).toContain(`<span class="nm">${SUGGESTED}</span>`)
    expect(en).not.toMatch(/>blank</)
    expect(html('graph', 'pl')).toMatch(/<span class="nm word">Pusty graf<\/span>/)
  })
  it('marks finished steps and the current step differently', () => {
    const out = html('agents')
    expect(out.match(/class="done"/g)).toHaveLength(2)
    expect(out.match(/class="cur"/g)).toHaveLength(1)
    const css = readFileSync(join(__dirname, '..', 'src', 'setup.css'), 'utf8')
    expect(css).toContain('.su-prog .done i b{transform:scaleX(1);background:var(--muted)}')
    expect(css).toContain('.su-prog .cur i{height:6px;border-radius:3px}')
  })
  it('keeps the check announcement region on every step, so a check that ends early is still announced', () => {
    for (const step of ['welcome', 'check', 'agents', 'graph'] as const) {
      const out = html(step)
      expect(out.match(/data-testid="cap-announce"/g), step).toHaveLength(1)
      expect(out, step).toMatch(/role="status" aria-live="polite" data-testid="cap-announce"/)
    }
  })
  it('shows what a template needs on its card before the report arrives', () => {
    expect(html('graph')).toContain('Needs Codex: not checked.')
  })
  it('draws the pulses under the nodes and icons, which are opaque (#236)', () => {
    const css = readFileSync(join(__dirname, '..', 'src', 'setup.css'), 'utf8')
    const rule = (sel: string) => css.match(new RegExp(`(?:^|\n)${sel.replace(/\./g, '\\.')}\\{([^}]*)\\}`))?.[1] ?? ''
    const z = (sel: string) => Number(/z-index:(\d+)/.exec(rule(sel))?.[1] ?? 'NaN')
    expect(z('.su-pulse')).toBeLessThan(z('.su-node'))
    expect(rule('.su-node')).toContain('background:var(--bg)')
    expect(rule('.su-path .pulse')).not.toContain('z-index')
    expect(rule('.su-path .su-mini')).toContain('position:relative')
    expect(z('.su-path .su-mini')).toBeGreaterThan(0)
    expect(rule('.su-path .su-mini')).toContain(',var(--bg)') // a solid layer under the translucent fill
  })
  it('is translated into Polish', () => {
    const out = html('graph', 'pl')
    expect(out).toContain('Wybierz pierwszy graf')
    expect(out).toContain('Pomiń konfigurację')
  })
})
