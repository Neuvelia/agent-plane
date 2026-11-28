import { useLayoutEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { isDirty, type DocState, type RunBlock } from './doc'
import { ErrorMark, Glyph, PlaySmall } from './glyphs'
import { GraphMenu } from './GraphMenu'
import { MOD } from './Menu'
import type { GraphDocument } from './useGraphDocument'

// The one header row of the graph editor v2: the graph's file name (its menu
// switches graphs and manages the file), the view switch, save state, undo and
// redo, Save, and the one primary action, Save and run. When the graph cannot
// run, the reason sits next to the button and opens the problem.

export type EditorView = 'visual' | 'yaml' | 'split'
export const VIEWS: EditorView[] = ['visual', 'yaml', 'split']

interface Props {
  g: GraphDocument
  doc: DocState
  view: EditorView
  onView: (v: EditorView) => void
  block: RunBlock | null
  errorCount: number
  onShowFirstError: () => void
  onRun: () => void
  onNew: () => void
}

export function HeaderBar({ g, doc, view, onView, block, errorCount, onShowFirstError, onRun, onNew }: Props) {
  const { t } = useI18n()
  const dirty = isDirty(doc)
  const saving = g.busy === 'saving'
  const blockText = block ? t(`editor.runBlocked.${block}`, { count: errorCount }) : undefined
  return (
    <div className="er-bar" role="toolbar" aria-label={t('editor.toolbar')}>
      <GraphMenu g={g} doc={doc} onNew={onNew} />
      <ViewSwitch view={view} onView={onView} />
      <span className="er-sp" />
      <span className="er-st" role="status" data-testid="doc-state">
        <span className={`dot ${doc.isNew ? '' : dirty ? 'warn' : 'ok'}`} aria-hidden="true" />
        {doc.isNew ? t('editor.notCreated') : dirty ? t('editor.unsaved') : t('editor.saved')}
      </span>
      <button type="button" className="btn ghost ib" aria-label={t('editor.undo')} title={`${t('editor.undo')} (${MOD}Z)`} onClick={g.undo} disabled={doc.past.length === 0} data-testid="editor-undo">
        <Glyph name="undo" size={18} />
      </button>
      <button type="button" className="btn ghost ib" aria-label={t('editor.redo')} title={`${t('editor.redo')} (⇧${MOD}Z)`} onClick={g.redo} disabled={doc.future.length === 0} data-testid="editor-redo">
        <Glyph name="redo" size={18} />
      </button>
      <span className="er-vr" />
      <button type="button" className="btn" onClick={() => void g.save()} disabled={saving || (!dirty && !doc.isNew)} data-testid="editor-save">
        {saving ? t('editor.saving') : t('editor.save')} <kbd>{MOD}S</kbd>
      </button>
      {block === 'errors' ? (
        <span data-testid="run-blocked" className="er-blk-wrap">
          <button id="er-blk" type="button" className="btn ghost er-blk" onClick={onShowFirstError} title={blockText} data-testid="run-blocked-focus">
            <ErrorMark />
            {t('editor.v2.blocksRun', { count: errorCount })}
          </button>
        </span>
      ) : block ? (
        <span id="er-blk" className="er-st er-blk-text" data-testid="run-blocked" title={blockText}>
          {t(`editor.v2.blocked.${block}`)}
        </span>
      ) : null}
      <button type="button" className="btn pr" disabled={block !== null || g.busy !== null} aria-describedby={block ? 'er-blk' : undefined} onClick={onRun} data-testid="editor-save-run">
        <PlaySmall />
        {g.busy === 'starting' ? t('editor.starting') : t('editor.saveRun')} <kbd>{MOD}↵</kbd>
      </button>
    </div>
  )
}

/** Visual / YAML / Split, with a thumb that slides to the chosen view. */
function ViewSwitch({ view, onView }: { view: EditorView; onView: (v: EditorView) => void }) {
  const { t } = useI18n()
  const refs = useRef<Record<string, HTMLButtonElement | null>>({})
  const [thumb, setThumb] = useState<{ x: number; w: number } | null>(null)
  useLayoutEffect(() => {
    const el = refs.current[view]
    if (el) setThumb({ x: el.offsetLeft, w: el.offsetWidth })
  }, [view, t])
  return (
    <div className="er-seg" role="group" aria-label={t('editor.view')}>
      {thumb && <span className="er-seg-thumb" style={{ transform: `translateX(${thumb.x - 2}px)`, width: thumb.w }} aria-hidden="true" />}
      {VIEWS.map((v) => (
        <button key={v} ref={(el) => void (refs.current[v] = el)} type="button" aria-pressed={view === v} onClick={() => onView(v)} data-testid={`view-${v}`}>
          {t(`editor.view.${v}`)}
        </button>
      ))}
    </div>
  )
}
