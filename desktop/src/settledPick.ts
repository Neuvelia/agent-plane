// F2.5e: arrow keys in a native radio group select each row they pass, and New
// run opens the graph it selects (a grant, a validation, a document read). A
// pick made with an arrow key is therefore shown at once but opened only when
// the selection rests; a click or Space opens at once.

/** The timer functions SettledPick uses; injected so tests need no clock. */
export interface Timers {
  set(run: () => void, ms: number): unknown
  clear(handle: unknown): void
}

/** How long an arrow-key selection must rest before it is opened. */
export const SETTLE_MS = 400

export class SettledPick<T> {
  private handle: unknown = null
  private waiting: T | null = null

  constructor(
    private readonly open: (value: T) => void,
    /** The value shown as selected while it waits, or null when nothing waits. */
    private readonly onWaiting: (value: T | null) => void,
    private readonly timers: Timers,
    private readonly delayMs = SETTLE_MS,
  ) {}

  /** A click or Space: open now, dropping anything still waiting. */
  now(value: T): void {
    this.cancel()
    this.open(value)
  }

  /** An arrow key moved the selection: show it, open it once it rests. */
  later(value: T): void {
    this.cancel()
    this.waiting = value
    this.onWaiting(value)
    this.handle = this.timers.set(() => {
      this.handle = null
      this.waiting = null
      this.onWaiting(null)
      this.open(value)
    }, this.delayMs)
  }

  /** Drops the waiting selection without opening it (back on the chosen graph, or leaving the screen). */
  cancel(): void {
    if (this.handle !== null) this.timers.clear(this.handle)
    this.handle = null
    if (this.waiting === null) return
    this.waiting = null
    this.onWaiting(null)
  }
}
