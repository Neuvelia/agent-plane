/** Epoch milliseconds of an event timestamp, or undefined when it is absent or unparseable. */
export const eventTimeMs = (iso: string | undefined): number | undefined => {
  if (!iso) return undefined
  const t = Date.parse(iso)
  return Number.isNaN(t) ? undefined : t
}

/** A non-negative interval in ms. Timestamps out of order (a clock step) give unknown, never a negative figure. */
export function interval(from: string | undefined, to: string | undefined): number | undefined {
  const a = eventTimeMs(from)
  const b = eventTimeMs(to)
  return a === undefined || b === undefined || b < a ? undefined : b - a
}
