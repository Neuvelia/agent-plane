import { describe, expect, it } from 'vitest'
import { MOD, focusForClick, focusForKey } from '../src/editor/Menu'

// The graph editor menus share the profile menu's key model (ui/menuKeys.ts):
// how a menu was opened decides where focus lands, with no global input state.

describe('editor menu opening focus', () => {
  it('a keyboard activation (click detail 0) focuses the first item; a pointer click focuses the menu box', () => {
    expect(focusForClick({ detail: 0 })).toBe('first')
    expect(focusForClick({ detail: 1 })).toBe('container')
  })
  it('arrow keys on the closed trigger open on the first or last item; other keys do nothing', () => {
    expect(focusForKey('ArrowDown')).toBe('first')
    expect(focusForKey('ArrowUp')).toBe('last')
    expect(focusForKey('Enter')).toBeNull()
  })
  it('the shortcut modifier is a key cap', () => {
    expect(['⌘', 'Ctrl']).toContain(MOD)
  })
})
