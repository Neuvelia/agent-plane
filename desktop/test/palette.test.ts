import { describe, expect, it } from 'vitest'
import { activeIndex, createCloser, outside, cycleScope, filterEntries, globalShortcut, IDLE, matchShortcut, MAX_RUNS, moveActive, paletteKey, parseQuery, readPaletteRuns, RECENT_RUNS, runEntries, SEQUENCE_WINDOW_MS, SHORTCUTS, shortcutText, type KeyPress, type PalettePress, type PaletteEntry, type PaletteGroup } from '../src/palette'

const label = (g: PaletteGroup) => ({ actions: 'Actions', runs: 'Runs', goto: 'Go to' })[g]
const runs = (n: number) => runEntries(Array.from({ length: n }, (_, i) => ({ run_id: `${String(i).padStart(2, '0')}c1f40a2-0000`, graph_id: `graph-${i}`, started_at: `2026-11-${String(10 + (i % 20)).padStart(2, '0')}T10:00:${String(i).padStart(2, '0')}Z`, status: 'failed' })), (s) => s)
const entries: PaletteEntry[] = [
  { id: 'new-run', group: 'actions', label: 'New run', hint: '' },
  { id: 'approvals', group: 'actions', label: 'Review approvals', hint: '2 waiting' },
  ...runEntries(
    [
      { run_id: '51b8c6f0-aaaa', graph_id: 'fix-issue', started_at: '2026-11-20T10:00:00Z', status: 'failed' },
      { run_id: '8c1f40a2-bbbb', graph_id: 'release-notes', started_at: '2026-11-21T10:00:00Z', status: 'running' },
    ],
    (s) => s,
  ),
  { id: 'go-inbox', group: 'goto', label: 'Inbox', hint: '' },
  { id: 'go-settings', group: 'goto', label: 'Settings', hint: '' },
]
const ids = (q: string, list = entries) => filterEntries(list, q, label).map((e) => e.id)

describe('palette filtering', () => {
  it('[F2.5d-AC2] shows every group in order with an empty query', () => {
    expect(ids('')).toEqual(['new-run', 'approvals', 'run:8c1f40a2-bbbb', 'run:51b8c6f0-aaaa', 'go-inbox', 'go-settings'])
  })
  it('matches label, hint and group name, ignoring case', () => {
    expect(ids('SETT')).toEqual(['go-settings'])
    expect(ids('waiting')).toEqual(['approvals'])
    expect(ids('go to')).toEqual(['go-inbox', 'go-settings'])
    expect(ids('51b8')).toEqual(['run:51b8c6f0-aaaa'])
  })
  it('[F2.5d-AC3] narrows to actions with ">" and to runs by id prefix with "#"', () => {
    expect(ids('>')).toEqual(['new-run', 'approvals'])
    expect(ids('> review')).toEqual(['approvals'])
    expect(ids('#')).toEqual(['run:8c1f40a2-bbbb', 'run:51b8c6f0-aaaa'])
    expect(ids('#8c1f')).toEqual(['run:8c1f40a2-bbbb'])
    // A run id is matched from its start, not anywhere inside it.
    expect(ids('#1f40')).toEqual([])
  })
  it('returns nothing when nothing matches', () => {
    expect(ids('zzz')).toEqual([])
  })
  it('bounds the runs: a few recent ones without a query, more with one', () => {
    const many = [...entries.filter((e) => e.group !== 'runs'), ...runs(40)]
    expect(filterEntries(many, '', label).filter((e) => e.group === 'runs')).toHaveLength(RECENT_RUNS)
    expect(filterEntries(many, 'graph', label)).toHaveLength(MAX_RUNS)
    expect(filterEntries(many, '#', label)).toHaveLength(MAX_RUNS)
  })
})

describe('palette scope', () => {
  it('parses the prefix and trims the text', () => {
    expect(parseQuery('  > New ')).toEqual({ scope: 'actions', text: 'new' })
    expect(parseQuery('#8C1F')).toEqual({ scope: 'runs', text: '8c1f' })
    expect(parseQuery('runs')).toEqual({ scope: 'all', text: 'runs' })
  })
  it('[F2.5d-AC3] Tab cycles all -> actions -> runs -> all, keeping the typed text; Shift+Tab goes back', () => {
    expect(cycleScope('fix', 1)).toBe('>fix')
    expect(cycleScope('>fix', 1)).toBe('#fix')
    expect(cycleScope('#fix', 1)).toBe('fix')
    expect(cycleScope('fix', -1)).toBe('#fix')
    expect(cycleScope('', 1)).toBe('>')
  })
})

describe('palette active row', () => {
  it('[F2.5d-AC2] moves with the arrows and wraps around', () => {
    expect(moveActive(0, 3, 'ArrowDown')).toBe(1)
    expect(moveActive(2, 3, 'ArrowDown')).toBe(0)
    expect(moveActive(0, 3, 'ArrowUp')).toBe(2)
    expect(moveActive(-1, 3, 'ArrowDown')).toBe(0)
    expect(moveActive(-1, 3, 'ArrowUp')).toBe(2)
  })
  it('has no active row in an empty list', () => {
    expect(moveActive(0, 0, 'ArrowDown')).toBe(-1)
  })
})

describe('run entries', () => {
  it('lists newest first with the graph as label and short id and status as hint', () => {
    const [first] = runEntries(
      [
        { run_id: 'aaaaaaaa1111', graph_id: 'old', started_at: '2026-11-01T00:00:00Z', status: 'succeeded' },
        { run_id: 'bbbbbbbb2222', graph_id: 'new', started_at: '2026-11-02T00:00:00Z', status: 'running' },
      ],
      (s) => s.toUpperCase(),
    )
    expect(first).toMatchObject({ id: 'run:bbbbbbbb2222', group: 'runs', label: 'new', hint: 'bbbbbbbb · RUNNING', runId: 'bbbbbbbb2222' })
  })
})

const press = (key: string, o: Partial<KeyPress> = {}): KeyPress => ({ key, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, typing: false, time: 1000, ...o })

describe('global shortcuts', () => {
  const idle = IDLE
  it('[F2.5d-AC1] Ctrl+K opens the palette on Windows and Linux, Cmd+K on macOS, even while typing', () => {
    expect(matchShortcut(idle, press('k', { ctrlKey: true, typing: true }), false).command).toBe('palette')
    expect(matchShortcut(idle, press('k', { metaKey: true }), true).command).toBe('palette')
    expect(matchShortcut(idle, press('k', { metaKey: true }), false).command).toBeNull()
    expect(matchShortcut(idle, press('k', { ctrlKey: true }), true).command).toBeNull()
  })
  it('maps the chords shown in the palette', () => {
    expect(matchShortcut(idle, press('n', { ctrlKey: true }), false).command).toBe('newRun')
    expect(matchShortcut(idle, press('o', { ctrlKey: true }), false).command).toBe('openGraph')
    expect(matchShortcut(idle, press('A', { ctrlKey: true, shiftKey: true }), false).command).toBe('approvals')
    expect(matchShortcut(idle, press(',', { ctrlKey: true }), false).command).toBe('settings')
    // Shift must match exactly; Alt never combines.
    expect(matchShortcut(idle, press('a', { ctrlKey: true }), false).command).toBeNull()
    expect(matchShortcut(idle, press('k', { ctrlKey: true, altKey: true }), false).command).toBeNull()
  })
  it('G then I goes to the Inbox, but never while typing', () => {
    const g = matchShortcut(idle, press('g'), false)
    expect(g).toEqual({ state: { pending: 'G', at: 1000 }, command: null })
    expect(matchShortcut(g.state, press('i'), false).command).toBe('inbox')
    expect(matchShortcut(g.state, press('x'), false)).toEqual({ state: IDLE, command: null })
    expect(matchShortcut(idle, press('g', { typing: true }), false).state.pending).toBeNull()
    expect(matchShortcut(g.state, press('i', { typing: true }), false).command).toBeNull()
  })
  it('writes shortcuts the way the operating system names them', () => {
    expect(shortcutText(SHORTCUTS.approvals, false)).toBe('Ctrl Shift A')
    expect(shortcutText(SHORTCUTS.approvals, true)).toBe('⌘ ⇧ A')
    expect(shortcutText(SHORTCUTS.settings, false)).toBe('Ctrl ,')
    expect(shortcutText(SHORTCUTS.inbox, true)).toBe('G I')
  })
})

describe('global shortcut decision', () => {
  it('drops a G that was pressed too long ago', () => {
    const g = matchShortcut(IDLE, press('g', { time: 1000 }), false).state
    expect(matchShortcut(g, press('i', { time: 1000 + SEQUENCE_WINDOW_MS }), false).command).toBe('inbox')
    expect(matchShortcut(g, press('i', { time: 1001 + SEQUENCE_WINDOW_MS }), false).command).toBeNull()
  })
  it('ignores presses another handler took, and auto-repeat, keeping the started sequence', () => {
    const g = matchShortcut(IDLE, press('g'), false).state
    expect(globalShortcut(g, press('k', { ctrlKey: true }), { handled: true, repeat: false, dialogOpen: false }, false)).toEqual({ state: g, command: null })
    expect(globalShortcut(g, press('k', { ctrlKey: true }), { handled: false, repeat: true, dialogOpen: false }, false)).toEqual({ state: g, command: null })
  })
  it('fires nothing while a modal dialog is open and forgets a started sequence', () => {
    const g = matchShortcut(IDLE, press('g'), false).state
    expect(globalShortcut(g, press('n', { ctrlKey: true }), { handled: false, repeat: false, dialogOpen: true }, false)).toEqual({ state: IDLE, command: null })
    // G pressed in a dialog, I after it closed: no navigation.
    const afterDialog = globalShortcut(g, press('x'), { handled: false, repeat: false, dialogOpen: true }, false).state
    expect(globalShortcut(afterDialog, press('i'), { handled: false, repeat: false, dialogOpen: false }, false).command).toBeNull()
  })
  it('passes every other press to the matcher', () => {
    expect(globalShortcut(IDLE, press('k', { ctrlKey: true }), { handled: false, repeat: false, dialogOpen: false }, false).command).toBe('palette')
  })
})

const inPalette = (key: string, o: Partial<PalettePress> = {}): PalettePress => ({ key, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, repeat: false, composing: false, ...o })

describe('keys inside the palette', () => {
  it('[F2.5d-AC2] arrows move, Enter chooses', () => {
    expect(paletteKey(inPalette('ArrowDown'), false)).toBe('next')
    expect(paletteKey(inPalette('ArrowUp'), false)).toBe('previous')
    expect(paletteKey(inPalette('Enter'), false)).toBe('choose')
    // A held arrow keeps moving; a held Enter does not choose again.
    expect(paletteKey(inPalette('ArrowDown', { repeat: true }), false)).toBe('next')
    expect(paletteKey(inPalette('Enter', { repeat: true }), false)).toBeNull()
  })
  it('[F2.5d-AC3] Tab and Shift+Tab change the scope', () => {
    expect(paletteKey(inPalette('Tab'), false)).toBe('scopeNext')
    expect(paletteKey(inPalette('Tab', { shiftKey: true }), false)).toBe('scopePrevious')
  })
  it('[F2.5d-AC1] the palette chord closes it, but not its auto-repeat', () => {
    expect(paletteKey(inPalette('k', { ctrlKey: true }), false)).toBe('close')
    expect(paletteKey(inPalette('k', { metaKey: true }), true)).toBe('close')
    expect(paletteKey(inPalette('k', { ctrlKey: true, repeat: true }), false)).toBeNull()
  })
  it('leaves keys alone while an input method composes text', () => {
    expect(paletteKey(inPalette('Enter', { composing: true }), false)).toBeNull()
    expect(paletteKey(inPalette('ArrowDown', { composing: true }), false)).toBeNull()
  })
  it('leaves typing and other chords to the field', () => {
    expect(paletteKey(inPalette('a'), false)).toBeNull()
    expect(paletteKey(inPalette('n', { ctrlKey: true }), false)).toBeNull()
  })
})

describe('active entry by id', () => {
  const shown = entries.slice(0, 3)
  it('[F2.5d-AC2] follows its entry when rows are inserted before it', () => {
    const later = [...shown.slice(0, 2), ...runs(5), shown[2]!]
    expect(activeIndex(shown, shown[2]!.id)).toBe(2)
    expect(activeIndex(later, shown[2]!.id)).toBe(7)
  })
  it('falls back to the first row when its entry is gone or none was chosen', () => {
    expect(activeIndex(shown, 'gone')).toBe(0)
    expect(activeIndex(shown, null)).toBe(0)
    expect(activeIndex([], null)).toBe(-1)
  })
})

describe('closing the palette', () => {
  const effects = () => {
    const log: string[] = []
    return { log, fx: { close: () => log.push('close'), onClose: (reopen: boolean) => log.push(`onClose:${reopen}`) }, cmd: () => log.push('command') }
  }
  it('closes the dialog, tells the shell, then runs the command once', () => {
    const { log, fx, cmd } = effects()
    const c = createCloser()
    expect(c.dismiss(cmd, true)).toBe('animate')
    expect(log).toEqual([])
    expect(c.finish(fx)).toBe(true)
    // animationend and the fallback timer may both arrive: the second finish does nothing.
    expect(c.finish(fx)).toBe(false)
    expect(log).toEqual(['close', 'onClose:false', 'command'])
  })
  it('finishes at once without an exit animation (reduced motion)', () => {
    expect(createCloser().dismiss(undefined, false)).toBe('finish')
  })
  it('ignores a second dismissal: Escape then Enter never runs a command', () => {
    const { log, fx, cmd } = effects()
    const c = createCloser()
    c.dismiss(undefined, true)
    expect(c.dismiss(cmd, true)).toBe('ignore')
    c.finish(fx)
    expect(log).toEqual(['close', 'onClose:false'])
  })
})

describe('reopening during the exit', () => {
  it('asks the shell for a fresh palette when the chord comes while closing', () => {
    const log: string[] = []
    const c = createCloser()
    expect(c.requestReopen()).toBe(false)
    c.dismiss(undefined, true)
    expect(c.requestReopen()).toBe(true)
    c.finish({ close: () => undefined, onClose: (reopen) => log.push(String(reopen)) })
    expect(log).toEqual(['true'])
    expect(c.requestReopen()).toBe(false)
  })
})

describe('backdrop by position', () => {
  const box = { left: 100, top: 110, right: 740, bottom: 600 }
  it('is outside the palette box only', () => {
    expect(outside(box, 20, 300)).toBe(true)
    expect(outside(box, 300, 700)).toBe(true)
    expect(outside(box, 300, 300)).toBe(false)
    // The border counts as the palette.
    expect(outside(box, 100, 110)).toBe(false)
  })
})

describe('reading runs for the palette', () => {
  it('[F2.5d-AC4] reports a failed read as an error, never as an empty list', async () => {
    expect(await readPaletteRuns(() => Promise.reject(new Error('sidecar down')))).toEqual({ status: 'error', runs: [] })
  })
  it('returns the runs that were read', async () => {
    const list = [{ run_id: 'a', graph_id: 'g', started_at: '2026-11-01T00:00:00Z', status: 'running' }]
    expect(await readPaletteRuns(() => Promise.resolve(list))).toEqual({ status: 'ready', runs: list })
  })
})
