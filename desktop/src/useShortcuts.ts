import { useEffect, useRef } from 'react'
import { matchShortcut, type GlobalCommand, type ShortcutState } from './palette'

// F2.5b global shortcuts (Ctrl/Cmd+K, +N, +O, +Shift+A, +comma, then G D).
// One window listener owned by the shell; the shell decides what each command does.

/** True when the key press lands in a field that takes text. */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return true
  return target instanceof HTMLInputElement && !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'].includes(target.type)
}

/**
 * Calls onCommand for a recognised shortcut. Ignored when another handler already
 * took the key, on auto-repeat, and while a modal dialog is open: the dialog owns the
 * keyboard then (the palette closes itself on its own shortcut).
 */
export function useGlobalShortcuts(mac: boolean, onCommand: (command: GlobalCommand) => void): void {
  const handler = useRef(onCommand)
  handler.current = onCommand
  useEffect(() => {
    let state: ShortcutState = { pending: null }
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.repeat) return
      const r = matchShortcut(state, { key: e.key, ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey, altKey: e.altKey, typing: isTypingTarget(e.target) }, mac)
      state = r.state
      if (!r.command || document.querySelector('dialog[open]')) return
      e.preventDefault()
      handler.current(r.command)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [mac])
}
