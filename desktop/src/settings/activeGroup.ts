/** A group of the Settings page that is in view, with its distance from the top of the view. */
export interface VisibleGroup<Id extends string = string> {
  id: Id
  top: number
}

/**
 * The active section of the scroll spy. Normally the visible group nearest the
 * top. At the end of the page the last group is too short to ever reach the
 * top band, so the last visible group in page order wins instead; `visible`
 * must then be in page order.
 */
export function activeGroup<Id extends string>(visible: readonly VisibleGroup<Id>[], atEnd: boolean): Id | null {
  if (visible.length === 0) return null
  if (atEnd) return visible[visible.length - 1]!.id
  return visible.reduce((nearest, g) => (g.top < nearest.top ? g : nearest)).id
}

/** Whether a scroll container shows the end of its content (within one pixel). */
export function isAtEnd(el: { scrollTop: number; clientHeight: number; scrollHeight: number }): boolean {
  return el.scrollTop + el.clientHeight >= el.scrollHeight - 1
}
