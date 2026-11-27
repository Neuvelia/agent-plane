import { filterQueue, type InboxFilter, type InboxItem } from './inbox'

// F2.5b: what the Inbox remembers about approvals decided on this screen. View
// state only: a decision is shown as recorded only once the run's events carry
// approval_decided (F2.10); this never decides or retries anything.

export interface Decisions {
  /** Approvals the person decided on this screen (the request succeeded or answered 409). */
  here: Record<string, InboxItem>
  /** Decisions the runs' events recorded (approval_decided), by item key, in whatever order they arrive. */
  seen: Record<string, string>
}

export const noDecisions: Decisions = { here: {}, seen: {} }

export function markDecidedHere(d: Decisions, item: InboxItem): Decisions {
  return d.here[item.key] ? d : { ...d, here: { ...d.here, [item.key]: item } }
}

/** Kept even before the request answers, so an event that beats the response is not lost. */
export function recordDecision(d: Decisions, key: string, decision: string): Decisions {
  return d.seen[key] === decision ? d : { ...d, seen: { ...d.seen, [key]: decision } }
}

/** The decision a queue row shows: only for an approval decided here whose events recorded it. */
export function shownDecision(d: Decisions, key: string): string | undefined {
  return d.here[key] ? d.seen[key] : undefined
}

/** Approvals decided here and confirmed by their events: they stay queued after the list drops them. */
export function heldInQueue(d: Decisions): InboxItem[] {
  return Object.values(d.here).filter((i) => d.seen[i.key] !== undefined)
}

/**
 * The item the detail shows: the picked one while it is in view; else an approval decided here
 * that the list dropped (its decision may still be on its way), if the filter shows its group;
 * else the first item in view.
 */
export function currentItem(visible: InboxItem[], queue: InboxItem[], picked: string | null, d: Decisions, filter: InboxFilter): InboxItem | undefined {
  const inView = visible.find((i) => i.key === picked)
  if (inView) return inView
  const held = picked ? d.here[picked] : undefined
  if (held && !queue.some((i) => i.key === held.key) && filterQueue([held], filter).length > 0) return held
  return visible[0]
}

/** Where Next goes: the first item in view that is not the current one and was not decided here; none when nothing is left. */
export function nextKey(visible: InboxItem[], current: string, d: Decisions): string | undefined {
  return visible.find((i) => i.key !== current && !d.here[i.key])?.key
}
