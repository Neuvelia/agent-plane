// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import * as templates from '../src/templates'
import { Welcome } from '../src/welcome'
import recorded from './fixtures/capabilities-linux.json'

// The four-step setup in a DOM (jsdom, as palette-dom.test.tsx): the Create button (F2.11) and
// the capability read, which must not repeat when only the language changes.

vi.mock('../src/templates', async (importOriginal) => {
  const real = await importOriginal<typeof import('../src/templates')>()
  return { ...real, templateDraft: vi.fn(real.templateDraft) }
})

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true

let root: Root
let host: HTMLDivElement
const request = vi.fn()
beforeEach(() => {
  request.mockReset().mockResolvedValue({ status: 200, body: recorded })
  Object.assign(window, { agentplane: { request } })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.mocked(templates.templateDraft).mockRestore?.()
})

const click = (testId: string) => act(() => void host.querySelector<HTMLElement>(`[data-testid="${testId}"]`)!.click())
const renderWelcome = async (onCreate = vi.fn(), locale: 'en' | 'pl' = 'en') => {
  const ui = (l: 'en' | 'pl') => (
    <I18nProvider locale={l}>
      <Welcome onCreate={onCreate} onOpen={() => undefined} onSkip={() => undefined} />
    </I18nProvider>
  )
  await act(async () => root.render(ui(locale)))
  return { onCreate, rerender: (l: 'en' | 'pl') => act(async () => root.render(ui(l))) }
}

describe('setup Create button', () => {
  it('creates the picked template through its draft maker', async () => {
    const { onCreate } = await renderWelcome()
    click('setup-progress-graph')
    click('welcome-template')
    expect(onCreate).toHaveBeenCalledTimes(1)
    const make = onCreate.mock.calls[0]![0] as (label: string) => string | null
    expect(typeof make).toBe('function')
    expect(make('my-graph')).toEqual(expect.stringContaining('my-graph'))
  })

  it('creates Blank with no draft maker', async () => {
    const { onCreate } = await renderWelcome()
    click('setup-progress-graph')
    click('setup-template-blank')
    click('welcome-template')
    expect(onCreate).toHaveBeenCalledTimes(1)
    expect(onCreate).toHaveBeenCalledWith(undefined)
  })

  it('does nothing while the picked template has no usable draft', async () => {
    const { onCreate } = await renderWelcome()
    click('setup-progress-graph')
    vi.mocked(templates.templateDraft).mockReturnValue(null)
    await act(async () => root.render(<I18nProvider locale="en"><Welcome onCreate={onCreate} onOpen={() => undefined} onSkip={() => undefined} initialStep="graph" /></I18nProvider>))
    const button = host.querySelector<HTMLButtonElement>('[data-testid="welcome-template"]')!
    expect(button.disabled).toBe(true)
    click('welcome-template')
    expect(onCreate).not.toHaveBeenCalled()
  })
})

describe('setup accessibility', () => {
  // The text a screen reader composes from aria-labelledby / aria-describedby (an img's aria-label counts as its text).
  const compose = (ids: string | null) =>
    (ids ?? '')
      .split(' ')
      .filter(Boolean)
      .map((id) => {
        const el = document.getElementById(id)
        expect(el, id).not.toBeNull()
        return (el!.getAttribute('aria-label') ?? el!.textContent ?? '').replace(/\s+/g, ' ').trim()
      })
  const radio = (id: string) => host.querySelector<HTMLInputElement>(`[data-testid="setup-template-${id}"]`)!
  const idsUnique = () => {
    const all = [...host.querySelectorAll('[id]')].map((e) => e.id)
    expect(new Set(all).size).toBe(all.length)
  }

  it('names a recommended template by its file name and badge, and describes it by its steps and meta line', async () => {
    await renderWelcome()
    click('setup-progress-graph')
    const input = radio('parallel-shell')
    const card = host.querySelector('[data-testid="setup-tpl-parallel-shell"]')!
    expect(card.querySelector('.su-rec')).not.toBeNull()
    expect(compose(input.getAttribute('aria-labelledby'))).toEqual(['parallel-shell', 'Recommended'])
    const described = compose(input.getAttribute('aria-describedby'))
    expect(described).toHaveLength(2)
    expect(described[0]).toMatch(/^Steps: /)
    expect(described[1]).toBe(card.querySelector('.su-meta')!.textContent!.replace(/\s+/g, ' ').trim())
    idsUnique()
  })

  it('also describes a template that needs something by its warnings, and names it without a badge', async () => {
    await renderWelcome()
    click('setup-progress-graph')
    const input = radio('dependency-update')
    const card = host.querySelector('[data-testid="setup-tpl-dependency-update"]')!
    const needs = card.querySelector('[data-testid="setup-needs"]')!
    expect(needs.textContent).toMatch(/\S/)
    expect(compose(input.getAttribute('aria-labelledby'))).toEqual(['dependency-update'])
    const described = compose(input.getAttribute('aria-describedby'))
    expect(described).toHaveLength(3)
    expect(described[2]).toBe(needs.textContent!.replace(/\s+/g, ' ').trim())
    // Blank: translated name, steps summary and meta, nothing else.
    expect(compose(radio('blank').getAttribute('aria-labelledby'))).toEqual(['Blank graph'])
    expect(compose(radio('blank').getAttribute('aria-describedby'))).toHaveLength(2)
    idsUnique()
  })
  it('shows Check again without an entrance delay, so a keyboard user never lands on it invisible', async () => {
    await renderWelcome()
    click('setup-progress-check')
    await act(async () => undefined)
    const button = host.querySelector<HTMLElement>('[data-testid="check-again"]')!
    expect(button.style.animationDelay).toBe('')
  })
})

describe('setup capability read', () => {
  it('reads the report once, and a language change does not read it again', async () => {
    const { rerender } = await renderWelcome()
    expect(request).toHaveBeenCalledTimes(1)
    await rerender('pl')
    expect(request).toHaveBeenCalledTimes(1)
    const announce = host.querySelector('[data-testid="cap-announce"]')!.textContent!
    expect(announce).toMatch(/^Sprawdzanie zakończone o .*\d/)
    expect(announce).not.toContain('Check finished')
    await rerender('en')
    expect(request).toHaveBeenCalledTimes(1)
    expect(host.querySelector('[data-testid="cap-announce"]')!.textContent).toMatch(/^Check finished at .*\d/)
  })
})
