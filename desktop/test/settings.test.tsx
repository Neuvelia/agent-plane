import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import recorded from './fixtures/capabilities-linux.json'
import type { CapabilityReport, CapabilityTool } from '../src/capabilities'
import { I18nProvider } from '../src/i18n/context'
import type { Locale } from '../src/i18n'
import { SECTIONS, Settings } from '../src/settings'
import { diagnosticsText } from '../src/settings/about'
import { AgentsPane, agentRows } from '../src/settings/agents'
import { EngineGroup } from '../src/settings/groups'
import { draftAfterToggle, namesDirty, noticeOf, sessionKey } from '../src/settings/recording'
import type { CapabilityView } from '../src/welcome'

const linux = recorded as CapabilityReport
const withTools = (tools: Partial<CapabilityTool>[]): CapabilityReport => ({ ...linux, tools: tools.map((t) => ({ id: 'x', kind: 'agent_cli', status: 'missing', version: '', reason: '', ...t })) })
const render = (node: ReactNode, locale: Locale = 'en') => renderToStaticMarkup(<I18nProvider locale={locale}>{node}</I18nProvider>)
const noop = () => undefined

describe('Settings sections', () => {
  it('lists the eight design sections in order, with Secrets and Policy inert [F2.5b-AC1]', () => {
    expect(SECTIONS.map((s) => s.id)).toEqual(['general', 'recording', 'execution', 'agents', 'engine', 'secrets', 'policy', 'about'])
    const out = render(<Settings />)
    for (const id of ['secrets', 'policy']) {
      const entry = new RegExp(`<(\\w+)[^>]*data-testid="settings-nav-${id}"[^>]*>`).exec(out)
      expect(entry?.[1], id).toBe('div')
      expect(entry?.[0], id).toContain('aria-disabled="true"')
      expect(entry?.[0], id).not.toMatch(/tabindex|onclick/i)
    }
    expect(out).toMatch(/<button[^>]*aria-current="true"[^>]*data-testid="settings-nav-general"/)
  })
  it('is localised', () => {
    const out = render(<Settings />, 'pl')
    expect(out).toContain('Ustawienia')
    expect(out).toContain('Wkrótce')
    expect(out).toContain('3 próby')
    expect(out).not.toContain('Record run values')
  })
})

describe('rows without a backing contract', () => {
  it('are disabled and say Soon; the retry budget shows the loop default [F2.5b-AC2]', () => {
    const out = render(<Settings />)
    const rows = out.split('<div class="st-row')
    for (const id of ['settings-graph-folder', 'settings-diffs', 'settings-store']) {
      const row = rows.find((r) => r.includes(`data-testid="${id}"`)) ?? ''
      expect(row, id).toContain('Soon')
      expect(row, id).toMatch(/disabled=""/)
    }
    expect(out).toMatch(/data-testid="settings-concurrency"[\s\S]*?<button[^>]*disabled=""[\s\S]*?>2<[\s\S]*?<button[^>]*disabled=""/)
    expect(out).toMatch(/data-testid="settings-retry">3 attempts</)
  })
})

describe('record run values', () => {
  it('is a native, labelled checkbox that starts off [F2.5b-AC3]', () => {
    const out = render(<Settings />)
    const box = /<input[^>]*data-testid="values-record"[^>]*>/.exec(out)?.[0] ?? ''
    expect(box).toContain('type="checkbox"')
    expect(box).not.toMatch(/\schecked/)
    const id = /id="([^"]+)"/.exec(box)?.[1]
    expect(out).toContain(`for="${id}"`)
  })
  it('maps save results and never reports a failed save as saved [F2.5b-AC3]', () => {
    expect(noticeOf({ ok: true, settings: { enabled: true, redactEnv: [], active: false, missing: [] } })).toEqual({ kind: 'saved' })
    expect(noticeOf({ ok: false, code: 'invalid', name: 'A=B' })).toEqual({ kind: 'invalid', name: 'A=B' })
    expect(noticeOf({ ok: false, code: 'save_failed' })).toEqual({ kind: 'failed' })
    expect(noticeOf(null)).toEqual({ kind: 'failed' })
  })
  it('tells a typed names draft from the saved list, so a toggle keeps an unsaved draft [F2.5b-AC3]', () => {
    expect(namesDirty('GITHUB_TOKEN', ['GITHUB_TOKEN'])).toBe(false)
    expect(namesDirty(' GITHUB_TOKEN, ', ['GITHUB_TOKEN'])).toBe(false)
    expect(namesDirty('GITHUB_TOKEN NPM_TOKEN', ['GITHUB_TOKEN'])).toBe(true)
    expect(namesDirty('NPM_TOKEN GITHUB_TOKEN', ['GITHUB_TOKEN', 'NPM_TOKEN'])).toBe(true)
    expect(namesDirty('', [])).toBe(false)
  })
  it('after a toggle, a clean draft follows the saved list and a typed draft stays [F2.5b-AC3]', () => {
    expect(draftAfterToggle('GITHUB_TOKEN', ['GITHUB_TOKEN'], ['GITHUB_TOKEN'])).toBe('GITHUB_TOKEN')
    expect(draftAfterToggle(' GITHUB_TOKEN,', ['GITHUB_TOKEN'], ['GITHUB_TOKEN'])).toBe('GITHUB_TOKEN')
    expect(draftAfterToggle('GITHUB_TOKEN NPM_TOKEN', ['GITHUB_TOKEN'], ['GITHUB_TOKEN'])).toBe('GITHUB_TOKEN NPM_TOKEN')
  })
  it('states next to the switch, visibly and in its description, that only listed variables are hidden (ADR-0023) [F2.5b-AC3]', () => {
    for (const [locale, text] of [['en', 'saved as it is'], ['pl', 'zapisują się bez zmian']] as const) {
      const out = render(<Settings />, locale)
      const box = /<input[^>]*data-testid="values-record"[^>]*>/.exec(out)?.[0] ?? ''
      const described = /aria-describedby="([^"]+)"/.exec(box)?.[1]?.split(' ') ?? []
      expect(described.length, locale).toBe(2)
      const warning = new RegExp(`<span id="${described[1]}" class="st-warn">([^<]+)</span>`).exec(out)?.[1] ?? ''
      expect(warning, locale).toContain(text)
      const details = /<details[\s\S]*?<\/details>/.exec(out)?.[0] ?? ''
      expect(details, locale).not.toContain(text)
    }
  })
  it('reports the session the running engine has', () => {
    expect(sessionKey({ enabled: true, redactEnv: [], active: true, missing: [] })).toBe('settings.values.sessionOn')
    expect(sessionKey({ enabled: true, redactEnv: ['A'], active: false, missing: ['A'] })).toBe('settings.values.sessionMissing')
    expect(sessionKey({ enabled: false, redactEnv: [], active: false, missing: [] })).toBe('settings.values.sessionOff')
  })
})

describe('engine status', () => {
  const status = (view: CapabilityView, lost = false) => /data-state="(\w+)"/.exec(render(<EngineGroup view={view} lost={lost} ring={null} onRingEnd={noop} />))?.[1]
  it('is not connected once the app reports the connection lost, even after a good read', () => {
    expect(status({ state: 'loaded', report: linux, refreshing: false }, true)).toBe('bad')
  })
  it('is connected for any answer, not connected only when unreachable', () => {
    expect(status({ state: 'loaded', report: linux, refreshing: false })).toBe('ok')
    expect(status({ state: 'failed', status: 503 })).toBe('ok')
    expect(status({ state: 'disconnected' })).toBe('bad')
    expect(status({ state: 'loading' })).toBe('checking')
  })
})

describe('Agents', () => {
  it('shows agent rows only, known agents in order and unknown ones last as received [F2.5b-AC4]', () => {
    const report = withTools([
      { id: 'git', kind: 'tool' },
      { id: 'newagent', kind: 'agent_cli' },
      { id: 'gemini' },
      { id: 'claude' },
      { id: 'codex', kind: 'agent_adapter' },
      { id: 'docker', kind: 'tool' },
    ])
    expect(agentRows(report).map((t) => t.id)).toEqual(['codex', 'claude', 'gemini', 'newagent'])
    const out = render(<AgentsPane view={{ state: 'loaded', report, refreshing: false }} onCheck={noop} onRetry={noop} />)
    expect(out).not.toContain('data-testid="agent-git"')
    expect(out).toContain('newagent')
  })
  it('lists the probe checks and the sign-in note for an available Codex [F2.5b-AC4]', () => {
    const report = withTools([{ id: 'codex', kind: 'agent_adapter', status: 'available', version: '0.169.0' }, { id: 'claude', status: 'detected_no_adapter' }])
    const out = render(<AgentsPane view={{ state: 'loaded', report, refreshing: false }} onCheck={noop} onRetry={noop} />)
    const codex = /data-testid="agent-codex"[\s\S]*?<\/section>/.exec(out)?.[0] ?? ''
    expect(codex).toContain('The file exists and can be run')
    expect(codex).toContain('version 0.169.0')
    expect(codex).toContain('cannot check that you are signed in')
    const claude = /data-testid="agent-claude"[\s\S]*?<\/section>/.exec(out)?.[0] ?? ''
    expect(claude).not.toContain('The file exists and can be run')
  })
  it('says when the engine cannot be reached, with Retry', () => {
    const out = render(<AgentsPane view={{ state: 'disconnected' }} onCheck={noop} onRetry={noop} />)
    expect(out).toContain('data-testid="agents-disconnected"')
    expect(out).toContain('data-testid="agents-retry"')
  })
})

describe('About', () => {
  it('copies versions and statuses only [F2.5b-AC5]', () => {
    const text = diagnosticsText('0.14.0', linux)
    expect(text.split('\n')[0]).toBe('app 0.14.0')
    expect(text).toContain('engine 0.0.0-dev linux/amd64')
    expect(text).toContain('codex not_configured (no_binary_configured)')
    expect(text).not.toMatch(/[/\\]{1}[a-z]+[/\\]/i)
    expect(diagnosticsText('', null)).toBe('app unknown')
  })
})
