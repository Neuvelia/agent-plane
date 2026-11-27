import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import recorded from './fixtures/capabilities-linux.json'
import { checkCapabilities, getCapabilities, markWelcomeDone, reasonKey, rowText, welcomeDone, WELCOME_KEY, type CapabilityReport, type CapabilityTool } from '../src/capabilities'
import { catalogues } from '../src/i18n'
import { I18nProvider } from '../src/i18n/context'
import { CapabilityPanel, type CapabilityView } from '../src/welcome'

// The closed sets of internal/capability (F2.11 requirements 2 and 5).
const STATUSES = ['available', 'missing', 'not_configured', 'unsupported_platform', 'unsupported_version', 'detected_no_adapter', 'error']
const REASONS = ['no_binary_configured', 'not_executable', 'probe_failed', 'probe_timeout', 'output_too_large', 'unrecognized_version', 'missing_required_flags', 'platform_not_supported', 'developer_tools_missing', 'found_on_path']
const tool = (t: Partial<CapabilityTool>): CapabilityTool => ({ id: 'git', kind: 'tool', status: 'available', version: '', reason: '', ...t })

describe('capability rows', () => {
  it('every status and reason has EN and PL text', () => {
    for (const s of STATUSES) for (const c of Object.values(catalogues)) expect(c[`cap.status.${s}` as keyof typeof c], s).toBeTruthy()
    for (const r of REASONS) {
      const k = reasonKey(r)
      expect(k, r).toBeDefined()
      for (const c of Object.values(catalogues)) expect(c[k!], r).toBeTruthy()
    }
    for (const id of ['engine', 'git', 'codex', 'claude', 'cursor-agent', 'gemini', 'shell', 'docker']) expect(catalogues.pl[`cap.tool.${id}` as keyof typeof catalogues.pl], id).toBeTruthy()
  })

  it('maps the agent CLI and Codex states the owner decision names', () => {
    expect(rowText(tool({ id: 'claude', kind: 'agent_cli', status: 'detected_no_adapter' }))).toMatchObject({ glyph: '◐', meaning: 'cap.meaning.cli.found', still: 'cap.still.cli' })
    expect(rowText(tool({ id: 'gemini', kind: 'agent_cli', status: 'missing' }))).toMatchObject({ glyph: '✕', meaning: 'cap.meaning.cli.missing' })
    expect(rowText(tool({ id: 'codex', kind: 'agent_adapter', status: 'not_configured', reason: 'found_on_path' })).meaning).toBe('cap.meaning.codex.foundOnPath')
    expect(rowText(tool({ id: 'codex', kind: 'agent_adapter', status: 'not_configured', reason: 'no_binary_configured' })).meaning).toBe('cap.meaning.codex.notConfigured')
    expect(rowText(tool({ id: 'claude', kind: 'agent_adapter', status: 'available' }))).toMatchObject({ glyph: '✓', meaning: 'cap.meaning.claude.available' })
    expect(rowText(tool({ id: 'claude', kind: 'agent_adapter', status: 'not_configured', reason: 'found_on_path' })).meaning).toBe('cap.meaning.claude.foundOnPath')
    expect(rowText(tool({ id: 'claude', kind: 'agent_adapter', status: 'not_configured', reason: 'no_binary_configured' })).meaning).toBe('cap.meaning.claude.notConfigured')
    expect(rowText(tool({ id: 'claude', kind: 'agent_adapter', status: 'missing' })).meaning).toBe('cap.meaning.claude.missing')
    expect(rowText(tool({ id: 'claude', kind: 'agent_adapter', status: 'unsupported_version' })).meaning).toBe('cap.meaning.claude.version')
    expect(rowText(tool({ id: 'claude', kind: 'agent_adapter', status: 'unsupported_platform' })).meaning).toBe('cap.meaning.claude.platform')
    expect(rowText(tool({ status: 'missing', reason: 'developer_tools_missing' })).meaning).toBe('cap.meaning.git.devtools')
    expect(rowText(tool({ status: 'available' })).still).toBeUndefined()
  })

  it('maps the shell and optional Docker rows (#188)', () => {
    expect(rowText(tool({ id: 'shell', kind: 'tool', status: 'available' }))).toMatchObject({ meaning: 'cap.meaning.shell.available' })
    expect(rowText(tool({ id: 'shell', kind: 'tool', status: 'missing' })).meaning).toBe('cap.meaning.shell.missing')
    expect(rowText(tool({ id: 'shell', kind: 'tool', status: 'unsupported_platform', reason: 'platform_not_supported' })).meaning).toBe('cap.meaning.shell.platform')
    expect(rowText(tool({ id: 'shell', kind: 'tool', status: 'error', reason: 'not_executable' })).still).toBe('cap.still.noShell')
    expect(rowText(tool({ id: 'docker', kind: 'tool', status: 'available' })).meaning).toBe('cap.meaning.docker.available')
    expect(rowText(tool({ id: 'docker', kind: 'tool', status: 'missing' }))).toMatchObject({ meaning: 'cap.meaning.docker.missing' })
  })

  it('shows an unknown status, reason or id verbatim and never as available', () => {
    const r = rowText(tool({ id: 'newcli', kind: 'agent_cli', status: 'quantum', reason: 'later' }))
    expect(r).toMatchObject({ name: { raw: 'newcli' }, status: { raw: 'quantum' }, glyph: '?', known: false, meaning: 'cap.meaning.unknown' })
    expect(reasonKey('later')).toBeUndefined()
  })
})

describe('capability requests', () => {
  afterEach(() => vi.unstubAllGlobals())
  const bridge = (request: () => Promise<unknown>) => vi.stubGlobal('window', { agentplane: { request } })

  it('accepts a report, sends the check with an empty object', async () => {
    const request = vi.fn(async () => ({ status: 200, body: recorded }))
    bridge(request)
    expect((await getCapabilities()).kind).toBe('ok')
    expect((await checkCapabilities()).kind).toBe('ok')
    expect(request.mock.calls).toEqual([['GET', '/capabilities', undefined], ['POST', '/capabilities/check', {}]])
  })
  it('a non-200 answer or a body that is not a report is a failure, never an empty list', async () => {
    bridge(async () => ({ status: 503, body: { error: { code: 'capabilities_unavailable' } } }))
    expect(await getCapabilities()).toEqual({ kind: 'failed', status: 503 })
    bridge(async () => ({ status: 200, body: { version: 1, tools: 'x' } }))
    expect(await getCapabilities()).toEqual({ kind: 'failed', status: 200 })
  })
  it('a rejected bridge call means the engine is unreachable', async () => {
    bridge(async () => {
      throw new Error('the agentplane sidecar is not running')
    })
    expect(await checkCapabilities()).toEqual({ kind: 'unreachable' })
  })
})

describe('first-run flag', () => {
  it('is unset in empty storage and set after completion', () => {
    const m = new Map<string, string>()
    const s = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) }
    expect(welcomeDone(s)).toBe(false)
    markWelcomeDone(s)
    expect(m.get(WELCOME_KEY)).toBe('done')
    expect(welcomeDone(s)).toBe(true)
  })
  it('treats throwing or missing storage as not completed, without an error', () => {
    const throwing = {
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
    }
    expect(welcomeDone(throwing)).toBe(false)
    expect(() => markWelcomeDone(throwing)).not.toThrow()
    expect(welcomeDone(undefined)).toBe(false)
  })
})

describe('capability panel states', () => {
  const html = (view: CapabilityView, locale: 'en' | 'pl' = 'en') =>
    renderToStaticMarkup(
      <I18nProvider locale={locale}>
        <CapabilityPanel view={view} appVersion="1.2.3" onCheck={() => undefined} onRetry={() => undefined} />
      </I18nProvider>,
    )
  it('request failed and disconnected render no rows', () => {
    for (const view of [{ state: 'failed', status: 503 }, { state: 'disconnected' }] as CapabilityView[]) {
      const out = html(view)
      expect(out).not.toContain('cap-row')
      expect(out).toContain('role="alert"')
    }
  })
  it('loaded shows the Engine row with both versions, then every tool with a status word', () => {
    const out = html({ state: 'loaded', report: recorded as CapabilityReport, refreshing: false })
    expect(out).toContain('engine 0.0.0-dev · app 1.2.3')
    for (const id of ['engine', 'git', 'codex', 'claude', 'cursor-agent', 'gemini', 'shell', 'docker']) expect(out).toContain(`data-testid="cap-${id}"`)
    expect(out).toContain('Not configured')
  })
  it('loading and re-checking mark the list busy; re-checking keeps the previous rows', () => {
    expect(html({ state: 'loading' })).toContain('aria-busy="true"')
    const out = html({ state: 'loaded', report: recorded as CapabilityReport, refreshing: true }, 'pl')
    expect(out).toContain('aria-busy="true"')
    expect(out).toContain('data-testid="cap-git"')
    expect(out).toContain('Sprawdzanie…')
  })
})

describe('no direct network access in the welcome code', () => {
  it('uses only the preload bridge', () => {
    for (const f of ['capabilities.ts', 'welcome.tsx', 'setup.ts', 'setupCheck.tsx', 'setupSteps.tsx']) {
      const text = readFileSync(join(__dirname, '..', 'src', f), 'utf8')
      expect(text, f).not.toMatch(/\bfetch\(|XMLHttpRequest|WebSocket|EventSource/)
    }
  })
})
