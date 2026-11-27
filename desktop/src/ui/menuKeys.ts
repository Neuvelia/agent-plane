// Keyboard model of a WAI-ARIA menu (F2.5b profile menu): which item takes
// focus for a key, and what the key does to the open submenu. Pure, so the
// behaviour is tested without a browser.

/** The next focused index for a navigation key, or null when the key does not move focus. Wraps at both ends. */
export function moveFocus(key: string, current: number, count: number): number | null {
  if (count === 0) return null
  switch (key) {
    case 'ArrowDown':
      return current < 0 ? 0 : (current + 1) % count
    case 'ArrowUp':
      return current < 0 ? count - 1 : (current - 1 + count) % count
    case 'Home':
      return 0
    case 'End':
      return count - 1
    default:
      return null
  }
}

/** Keys that close a submenu and return focus to its trigger. */
export function closesSubmenu(key: string): boolean {
  return key === 'ArrowLeft' || key === 'Escape'
}

/** Arrow keys on the closed menu button open the menu and pick the item that takes focus.
 *  Enter and Space reach the button as a native click, which opens it on the first item. */
export function openFromButton(key: string): 'first' | 'last' | null {
  if (key === 'ArrowDown') return 'first'
  if (key === 'ArrowUp') return 'last'
  return null
}

/** The modifier label shown in shortcuts: Cmd on macOS, Ctrl elsewhere. */
export function modifierLabel(platform: string): 'Cmd' | 'Ctrl' {
  return /Mac|iPhone|iPad/i.test(platform) ? 'Cmd' : 'Ctrl'
}

/** True when the platform's command modifier is held: Cmd on macOS, Ctrl elsewhere. */
export function commandHeld(e: Pick<KeyboardEvent, 'ctrlKey' | 'metaKey'>, platform: string): boolean {
  return modifierLabel(platform) === 'Cmd' ? e.metaKey : e.ctrlKey
}
