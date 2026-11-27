import { useEffect, useLayoutEffect, useRef } from 'react'
import { globalShortcut, IDLE, type GlobalCommand, type ShortcutState } from './palette'

// F2.5d global shortcuts (Ctrl/Cmd+K, +N, +O, +Shift+A, +comma, then G I).
// One window listener owned by the shell; the shell decides what each command does.

/** True when the key press lands in a field that takes text. */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return true
  return target instanceof HTMLInputElement && !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'].includes(target.type)
}

/** True while a modal dialog (the palette, the template picker, About, the shortcut list) owns the keyboard; one already animating out does not (F2.5c). */
export function modalOpen(): boolean {
  return document.querySelector('dialog[open]:not(.closing)') !== null
}

/**
 * Calls onCommand for a recognised shortcut. The decision is globalShortcut's: nothing
 * fires for a press another handler took, on auto-repeat, or while a modal dialog is open
 * (the palette closes itself on its own shortcut).
 */
export function useGlobalShortcuts(mac: boolean, onCommand: (command: GlobalCommand) => void): void {
  const handler = useRef(onCommand)
  // Updated after render, not during it; the listener always calls the latest handler.
  useLayoutEffect(() => {
    handler.current = onCommand
  })
  useEffect(() => {
    let state: ShortcutState = IDLE
    const onKey = (e: KeyboardEvent) => {
      const press = { key: e.key, ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey, altKey: e.altKey, typing: isTypingTarget(e.target), time: e.timeStamp }
      const r = globalShortcut(state, press, { handled: e.defaultPrevented, repeat: e.repeat, dialogOpen: modalOpen() }, mac)
      state = r.state
      if (!r.command) return
      e.preventDefault()
      handler.current(r.command)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [mac])
}
