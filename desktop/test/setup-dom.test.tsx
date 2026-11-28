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
  it('names each template radio by its name span, and describes it once by its meta text', async () => {
    await renderWelcome()
    click('setup-progress-graph')
    for (const input of host.querySelectorAll<HTMLInputElement>('input[name="setup-template"]')) {
      const name = document.getElementById(input.getAttribute('aria-labelledby')!)
      expect(name?.classList.contains('nm')).toBe(true)
      expect(document.getElementById(input.getAttribute('aria-describedby')!)?.classList.contains('su-meta')).toBe(true)
    }
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
    expect(host.querySelector('[data-testid="cap-announce"]')!.textContent).toMatch(/\d/)
  })
})
