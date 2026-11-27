// @vitest-environment jsdom
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '../src/i18n/context'
import { SHORTCUTS, type GlobalCommand, type PaletteRun } from '../src/palette'
import { CommandPalette, usePaletteRuns, type PaletteCommand, type RunsState } from '../src/ui/CommandPalette'
import { useGlobalShortcuts } from '../src/useShortcuts'

// F2.5b: the palette component and the shortcut hook in a DOM (jsdom). jsdom has no
// <dialog> methods, so a minimal stand-in opens/closes it and restores focus as browsers do.
// Animations do not run in jsdom: tests fire animationend, advance the fallback timer, or
// report no animation (reduced motion) through getComputedStyle.

// React decides at load time whether to listen for "animationend"; jsdom lacks AnimationEvent.
vi.hoisted(() => {
  if (!('AnimationEvent' in window)) Object.assign(window, { AnimationEvent: class AnimationEvent extends Event {} })
})

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true

const restore = new WeakMap<HTMLDialogElement, Element | null>()
beforeEach(() => {
  Object.assign(HTMLDialogElement.prototype, {
    showModal(this: HTMLDialogElement) {
      restore.set(this, document.activeElement)
      this.setAttribute('open', '')
    },
    close(this: HTMLDialogElement) {
      if (!this.hasAttribute('open')) return
      this.removeAttribute('open')
      ;(restore.get(this) as HTMLElement | null)?.focus?.()
    },
  })
  Element.prototype.scrollIntoView = vi.fn()
})

let root: Root
let host: HTMLDivElement
beforeEach(() => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

const log: string[] = []
beforeEach(() => {
  log.length = 0
})
const command = (id: string, group: PaletteCommand['group'], label: string, extra: Partial<PaletteCommand> = {}): PaletteCommand => ({ id, group, label, hint: '', icon: 'runs', perform: () => log.push(`perform:${id}`), ...extra })
const actions = [command('new-run', 'actions', 'New run', { shortcut: SHORTCUTS.newRun }), command('approvals', 'actions', 'Review approvals')]
const gotos = [command('go-dashboard', 'goto', 'Dashboard'), command('go-settings', 'goto', 'Settings')]
const runRows = [command('run:aaaa1111', 'runs', 'release-notes', { runId: 'aaaa1111' }), command('run:bbbb2222', 'runs', 'fix-issue', { runId: 'bbbb2222' })]

function mount(commands: PaletteCommand[] = [...actions, ...gotos], runs: RunsState = 'ready') {
  const onClose = (reopen: boolean) => log.push(`onClose:${reopen}`)
  const render = (c: PaletteCommand[], r: RunsState = runs) =>
    act(() =>
      root.render(
        <I18nProvider locale="en">
          <CommandPalette commands={c} runs={r} mac={false} onClose={onClose} />
        </I18nProvider>,
      ),
    )
  render(commands)
  return { render }
}

const $ = <T extends Element = HTMLElement>(sel: string) => document.querySelector<T>(sel)!
const dialog = () => $<HTMLDialogElement>('dialog.cmdk')
const input = () => $<HTMLInputElement>('[data-testid="palette-input"]')
const selected = () => $('[role="option"][aria-selected="true"]').getAttribute('data-testid')
function key(k: string, init: KeyboardEventInit = {}, target: Element = input()) {
  act(() => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init }))
  })
}
function type(text: string) {
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input(), text)
    input().dispatchEvent(new Event('input', { bubbles: true }))
  })
}
function animationEnd(name: string) {
  act(() => {
    const e = new Event('animationend', { bubbles: true })
    Object.defineProperty(e, 'animationName', { value: name })
    dialog().dispatchEvent(e)
  })
}
/** What the browser reports for the dialog's animation: "none" under reduced motion. jsdom has no animations of its own. */
function animation(name: 'cmdk-in' | 'none') {
  const real = window.getComputedStyle
  vi.spyOn(window, 'getComputedStyle').mockImplementation((el) => Object.assign(real(el), { animationName: name }))
}
const noAnimation = () => animation('none')
const withAnimation = () => animation('cmdk-in')
function mouse(type: 'mousedown' | 'click', target: Element, x: number, y: number) {
  const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y })
  act(() => {
    target.dispatchEvent(e)
  })
  return e
}

describe('choosing and closing', () => {
  it('[F2.5b-AC2] Enter closes the dialog, tells the shell, then runs the active command exactly once', () => {
    withAnimation()
    vi.useFakeTimers()
    mount()
    key('ArrowDown')
    key('Enter')
    expect(dialog().classList.contains('closing')).toBe(true)
    expect(log).toEqual([])
    animationEnd('cmdk-out')
    expect(log).toEqual(['onClose:false', 'perform:approvals'])
    // The fallback timer fires later and must not run anything again.
    act(() => vi.advanceTimersByTime(1000))
    key('Enter')
    expect(log).toEqual(['onClose:false', 'perform:approvals'])
    expect(dialog().open).toBe(false)
  })
  it('ignores the end of other animations (entrance, backdrop) while closing', () => {
    withAnimation()
    mount()
    key('Enter')
    animationEnd('cmdk-in')
    animationEnd('cmdk-fade-out')
    expect(log).toEqual([])
    animationEnd('cmdk-out')
    expect(log).toEqual(['onClose:false', 'perform:new-run'])
  })
  it('closes after the fallback when no animationend arrives (a hidden window)', () => {
    withAnimation()
    vi.useFakeTimers()
    mount()
    key('Escape', {}, input())
    act(() => {
      dialog().dispatchEvent(new Event('cancel', { cancelable: true }))
    })
    expect(log).toEqual([])
    act(() => vi.advanceTimersByTime(299))
    expect(log).toEqual([])
    act(() => vi.advanceTimersByTime(1))
    expect(log).toEqual(['onClose:false'])
  })
  it('closes at once with reduced motion (no exit animation)', () => {
    noAnimation()
    mount()
    key('Enter')
    expect(log).toEqual(['onClose:false', 'perform:new-run'])
  })
  it('returns focus to where it was', () => {
    noAnimation()
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    mount()
    act(() => input().focus())
    act(() => {
      dialog().dispatchEvent(new Event('cancel', { cancelable: true }))
    })
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })
  it('[F2.5b-AC1] the palette chord closes it; pressed again during the exit it asks for a fresh palette', () => {
    withAnimation()
    mount()
    key('k', { ctrlKey: true })
    expect(dialog().classList.contains('closing')).toBe(true)
    key('k', { ctrlKey: true })
    animationEnd('cmdk-out')
    expect(log).toEqual(['onClose:true'])
  })
  it('does nothing on Enter while an input method composes text', () => {
    noAnimation()
    mount()
    key('Enter', { isComposing: true })
    expect(log).toEqual([])
    expect(dialog().open).toBe(true)
  })
})

describe('keyboard and active entry', () => {
  it('[F2.5b-AC2] keeps the active entry when runs arrive above it', () => {
    noAnimation()
    const { render } = mount([...actions, ...gotos], 'loading')
    key('ArrowDown')
    key('ArrowDown')
    expect(selected()).toBe('palette-item-go-dashboard')
    render([...actions, ...runRows, ...gotos], 'ready')
    expect(selected()).toBe('palette-item-go-dashboard')
    key('Enter')
    expect(log).toEqual(['onClose:false', 'perform:go-dashboard'])
  })
  it('[F2.5b-AC3] Tab rewrites the prefix and the first entry becomes active', () => {
    mount([...actions, ...runRows, ...gotos])
    key('ArrowDown')
    key('Tab')
    expect(input().value).toBe('>')
    expect(selected()).toBe('palette-item-new-run')
    expect([...document.querySelectorAll('[role="option"]')].map((o) => o.getAttribute('data-testid'))).toEqual(['palette-item-new-run', 'palette-item-approvals'])
  })
  it('scrolls the active row into view when the query changes, not on other renders', () => {
    const { render } = mount()
    const spy = Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>
    spy.mockClear()
    render([...actions, ...gotos])
    expect(spy).not.toHaveBeenCalled()
    type('dash')
    expect(spy).toHaveBeenCalled()
  })
})

describe('mouse', () => {
  const box = { left: 100, top: 110, right: 740, bottom: 600, x: 100, y: 110, width: 640, height: 490, toJSON: () => ({}) }
  const backdrop = () => vi.spyOn(dialog(), 'getBoundingClientRect').mockReturnValue(box as DOMRect)
  beforeEach(noAnimation)

  it('keeps focus in the field when a heading, a row or the footer is pressed', () => {
    mount()
    act(() => input().focus())
    expect(mouse('mousedown', $('.cmdk-g'), 200, 200).defaultPrevented).toBe(true)
    expect(mouse('mousedown', $('.cmdk-foot'), 200, 590).defaultPrevented).toBe(true)
    expect(mouse('mousedown', $('[role="option"]'), 200, 220).defaultPrevented).toBe(true)
    expect(mouse('mousedown', input(), 200, 140).defaultPrevented).toBe(false)
  })
  it('lets the listbox take the press (its scrollbar) and hands focus back to the field', () => {
    mount()
    expect(mouse('mousedown', $('[role="listbox"]'), 735, 300).defaultPrevented).toBe(false)
    act(() => $('[role="listbox"]').focus())
    expect(document.activeElement).toBe(input())
  })
  it('closes on a click that starts and ends on the backdrop', () => {
    mount()
    backdrop()
    mouse('mousedown', dialog(), 20, 20)
    mouse('click', dialog(), 20, 20)
    expect(log).toEqual(['onClose:false'])
  })
  it('stays open when a press on the backdrop is released inside (click lands on the dialog)', () => {
    mount()
    backdrop()
    mouse('mousedown', dialog(), 20, 20)
    mouse('click', dialog(), 300, 300)
    expect(log).toEqual([])
  })
  it('stays open when a press inside is released on the backdrop', () => {
    mount()
    backdrop()
    mouse('mousedown', input(), 300, 140)
    mouse('click', dialog(), 20, 20)
    expect(log).toEqual([])
  })
})

describe('live region', () => {
  const status = () => $('[data-testid="palette-status"]')
  it('stays mounted and says the number of results, the scope, no match, or the run list state', () => {
    mount([...actions, ...runRows, ...gotos])
    const region = status()
    expect(region.getAttribute('role')).toBe('status')
    expect(region.textContent).toBe('6 results')
    type('>')
    expect(region.textContent).toBe('Actions only. 2 results')
    type('#bbbb')
    expect(region.textContent).toBe('Runs by id. 1 result')
    type('zzz')
    expect(region.textContent).toBe('Nothing matches “zzz”. Try a run id or a graph name.')
    expect(status()).toBe(region)
    // Visible messages are not live regions of their own.
    expect($('[data-testid="palette-none"]').getAttribute('role')).toBeNull()
  })
  it('[F2.5b-AC4] says when runs are loading or could not be read', () => {
    const { render } = mount([...actions, ...gotos], 'loading')
    expect(status().textContent).toBe('Loading runs…')
    render([...actions, ...gotos], 'error')
    expect(status().textContent).toBe('Runs could not be loaded. Close and reopen the palette to try again.')
  })
})

function RunsProbe({ session, read, out }: { session: number | null; read: () => Promise<PaletteRun[]>; out: { status: string; runs: PaletteRun[] }[] }) {
  out.push(usePaletteRuns(session, read))
  return null
}

describe('usePaletteRuns', () => {
  const run: PaletteRun = { run_id: 'a', graph_id: 'g', started_at: '2026-11-01T00:00:00Z', status: 'running' }
  it('[F2.5b-AC4] reports a failed read as an error', async () => {
    const out: { status: string; runs: PaletteRun[] }[] = []
    await act(async () => root.render(<RunsProbe session={1} read={() => Promise.reject(new Error('down'))} out={out} />))
    expect(out.at(-1)).toEqual({ status: 'error', runs: [] })
  })
  it('never shows runs from an earlier opening: a new session starts from loading and reads again', async () => {
    const out: { status: string; runs: PaletteRun[] }[] = []
    let calls = 0
    let answer: (r: PaletteRun[]) => void = () => undefined
    const read = () => {
      calls++
      return calls === 1 ? Promise.resolve([run]) : new Promise<PaletteRun[]>((r) => (answer = r))
    }
    await act(async () => root.render(<RunsProbe session={1} read={read} out={out} />))
    expect(out.at(-1)).toEqual({ status: 'ready', runs: [run] })
    await act(async () => root.render(<RunsProbe session={null} read={read} out={out} />))
    out.length = 0
    await act(async () => root.render(<RunsProbe session={2} read={read} out={out} />))
    // Every render of session 2 before its answer is "loading" with no runs.
    expect(out.every((s) => s.status === 'loading' && s.runs.length === 0)).toBe(true)
    expect(calls).toBe(2)
    await act(async () => answer([]))
    expect(out.at(-1)).toEqual({ status: 'ready', runs: [] })
  })
})

function ShortcutProbe({ out }: { out: GlobalCommand[] }) {
  const [, force] = useState(0)
  useGlobalShortcuts(false, (c) => {
    out.push(c)
    force((n) => n + 1)
  })
  return <input data-testid="field" />
}

describe('useGlobalShortcuts', () => {
  const press = (k: string, init: KeyboardEventInit = {}, target: EventTarget = document.body) =>
    act(() => {
      target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init }))
    })
  it('[F2.5b-AC1] answers the chords and G then D outside text fields', () => {
    const out: GlobalCommand[] = []
    act(() => root.render(<ShortcutProbe out={out} />))
    press('k', { ctrlKey: true })
    press('g')
    press('d')
    expect(out).toEqual(['palette', 'dashboard'])
  })
  it('ignores auto-repeat, presses another handler took, sequences typed in a field, and everything under a modal dialog', () => {
    const out: GlobalCommand[] = []
    act(() => root.render(<ShortcutProbe out={out} />))
    press('k', { ctrlKey: true, repeat: true })
    const taken = new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true })
    taken.preventDefault()
    act(() => {
      document.body.dispatchEvent(taken)
    })
    const field = $('[data-testid="field"]')
    press('g', {}, field)
    press('d', {}, field)
    const modal = document.createElement('dialog')
    document.body.append(modal)
    modal.showModal()
    press('n', { ctrlKey: true })
    modal.remove()
    expect(out).toEqual([])
  })
})
