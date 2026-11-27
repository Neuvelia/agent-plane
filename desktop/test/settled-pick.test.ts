import { describe, expect, it } from 'vitest'
import { SettledPick, type Timers } from '../src/settledPick'

// F2.5e: arrow keys select each recent graph they pass; only the one the selection
// rests on is opened. A fake clock is released by the test, no real waiting.
class FakeTimers implements Timers {
  private next = 0
  pending = new Map<number, () => void>()
  set = (run: () => void) => {
    this.pending.set(++this.next, run)
    return this.next
  }
  clear = (h: unknown) => void this.pending.delete(h as number)
  fire() {
    const due = [...this.pending.values()]
    this.pending.clear()
    for (const run of due) run()
  }
}

function setup() {
  const timers = new FakeTimers()
  const opened: string[] = []
  const shown: (string | null)[] = []
  const pick = new SettledPick<string>((v) => opened.push(v), (v) => shown.push(v), timers)
  return { timers, opened, shown, pick }
}

describe('SettledPick', () => {
  it('arrowing through several graphs opens only the one the selection rests on', () => {
    const { timers, opened, shown, pick } = setup()
    pick.later('a')
    pick.later('b')
    pick.later('c')
    expect(opened).toEqual([])
    expect(shown.filter((v) => v !== null)).toEqual(['a', 'b', 'c'])
    timers.fire()
    expect(opened).toEqual(['c'])
    expect(shown.at(-1)).toBeNull()
  })

  it('a click or Space opens at once and drops a waiting arrow selection', () => {
    const { timers, opened, pick } = setup()
    pick.later('a')
    pick.now('b')
    timers.fire()
    expect(opened).toEqual(['b'])
  })

  it('cancelling (back on the chosen graph, a run starting, leaving the screen) opens nothing', () => {
    const { timers, opened, shown, pick } = setup()
    pick.later('a')
    pick.cancel()
    timers.fire()
    expect(opened).toEqual([])
    expect(shown).toEqual(['a', null])
    expect(timers.pending.size).toBe(0)
  })
})
