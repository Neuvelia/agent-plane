import { defaultKeymap } from '@codemirror/commands'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { yaml } from '@codemirror/lang-yaml'
import { EditorState } from '@codemirror/state'
import { EditorView, drawSelection, highlightActiveLine, keymap, lineNumbers } from '@codemirror/view'
import { tags } from '@lezer/highlight'
import { useEffect, useRef } from 'react'

// The YAML view: CodeMirror 6, configured to be a plain text editor of the draft.
// It has no history of its own (the app keeps one undo stack for the canvas,
// forms and this view), no autocompletion and no linting: graph rules are the
// sidecar's. Tab is not bound, so the keyboard can always leave the editor.


// Colours come from the design tokens so every theme value is contrast-checked
// (test/contrast.test.ts) on `code-bg`.
const highlight = HighlightStyle.define([
  { tag: tags.propertyName, color: 'var(--code-key)' },
  { tag: [tags.string, tags.special(tags.string)], color: 'var(--code-string)' },
  { tag: [tags.number, tags.bool, tags.null], color: 'var(--code-number)' },
  { tag: [tags.labelName, tags.typeName, tags.meta, tags.keyword], color: 'var(--code-var)' },
  { tag: tags.comment, color: 'var(--muted)', fontStyle: 'italic' },
])

const theme = EditorView.theme(
  {
    '&': { backgroundColor: 'var(--code-bg)', color: 'var(--fg)', height: '100%', fontSize: '12.5px' },
    '&.cm-focused': { outline: '2px solid var(--focus)', outlineOffset: '2px' },
    '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '22px', overflow: 'auto' },
    '.cm-content': { caretColor: 'var(--fg)', padding: '12px 0' },
    '.cm-line': { padding: '0 12px 0 4px' },
    '.cm-gutters': { backgroundColor: 'transparent', color: 'var(--muted)', border: 'none' },
    '.cm-lineNumbers .cm-gutterElement': { padding: '0 14px 0 12px', minWidth: '32px' },
    '.cm-activeLine': { backgroundColor: 'var(--fill-4)' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--fg)' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--fg)' },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'var(--brand-soft)' },
  },
  { dark: true },
)

export interface YamlEditorProps {
  value: string
  onChange: (text: string) => void
  onSave: () => void
  onUndo: () => void
  onRedo: () => void
  /** The accessible name of the editor. */
  label: string
}

export function YamlEditor({ value, onChange, onSave, onUndo, onRedo, label }: YamlEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  // Handlers change every render; the editor is created once and reads the latest.
  const handlers = useRef({ onChange, onSave, onUndo, onRedo })
  handlers.current = { onChange, onSave, onUndo, onRedo }
  // True while the app pushes a new value into the editor, so it is not echoed back.
  const applying = useRef(false)

  useEffect(() => {
    if (!host.current) return
    const run = (fn: () => void) => () => {
      fn()
      return true
    }
    const v = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          highlightActiveLine(),
          drawSelection(),
          yaml(),
          syntaxHighlighting(highlight),
          theme,
          keymap.of([
            { key: 'Mod-s', run: run(() => handlers.current.onSave()), preventDefault: true },
            { key: 'Mod-z', run: run(() => handlers.current.onUndo()), preventDefault: true },
            { key: 'Mod-Shift-z', run: run(() => handlers.current.onRedo()), preventDefault: true },
            { key: 'Mod-y', run: run(() => handlers.current.onRedo()), preventDefault: true },
            ...defaultKeymap,
          ]),
          EditorView.contentAttributes.of({ 'aria-label': label, 'data-testid': 'yaml-content' }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !applying.current) handlers.current.onChange(update.state.doc.toString())
          }),
        ],
      }),
    })
    // Axe: a scrollable region must be keyboard reachable.
    v.scrollDOM.tabIndex = 0
    view.current = v
    return () => {
      v.destroy()
      view.current = null
    }
    // The editor is created once; later values arrive through the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // The draft changed outside the editor (undo, redo, reload): show it, keeping the caret in range.
  useEffect(() => {
    const v = view.current
    if (!v || v.state.doc.toString() === value) return
    applying.current = true
    const head = Math.min(v.state.selection.main.head, value.length)
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value }, selection: { anchor: head } })
    applying.current = false
  }, [value])

  return <div className="yaml-editor" ref={host} data-testid="yaml-editor" />
}
