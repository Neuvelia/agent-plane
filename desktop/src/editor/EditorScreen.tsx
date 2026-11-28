import { Suspense, lazy, useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import { GraphCanvas } from './GraphCanvas'
import { HeaderBar, VIEWS, type EditorView } from './HeaderBar'
import { ProblemsDrawer, StatusBar, issuesOf } from './IssuesPanel'
import { firstError, issueCounts, type FocusReq, type Issue } from './issues'
import { runBlock, type DocState } from './doc'
import { Glyph } from './glyphs'
import { selectionFor, useSelection } from './selection'
import { SidePanel } from './SidePanel'
import type { GraphDocument } from './useGraphDocument'
import { LoadingState } from '../states'
import { useValuesPicker } from '../useRunValues'
import { TemplatePicker } from './TemplatePicker'
import { MOD } from './Menu'

// The graph editor v2: one header row, a full-bleed canvas, the inspector on the
// right, and problems in a status bar that opens a drawer. The document, its
// history and every graph rule stay where they were (doc.ts, useGraphDocument.ts,
// the engine); this file only lays them out.

// CodeMirror is loaded when the editor is first shown, not at application start.
const YamlEditor = lazy(() => import('./YamlEditor').then((m) => ({ default: m.YamlEditor })))

const VIEW_KEY = 'agentplane.editor.view'
function loadView(): EditorView {
  try {
    const v = localStorage.getItem(VIEW_KEY)
    return (VIEWS as string[]).includes(v ?? '') ? (v as EditorView) : 'visual'
  } catch {
    return 'visual'
  }
}
function saveView(v: EditorView) {
  try {
    localStorage.setItem(VIEW_KEY, v)
  } catch {
    // Only the remembered view is lost.
  }
}

const DRAWER_KEY = 'agentplane.editor.problems'
function loadDrawer(): boolean | null {
  try {
    const v = localStorage.getItem(DRAWER_KEY)
    return v === 'open' ? true : v === 'closed' ? false : null
  } catch {
    return null
  }
}
function saveDrawer(open: boolean) {
  try {
    localStorage.setItem(DRAWER_KEY, open ? 'open' : 'closed')
  } catch {
    // Only the remembered choice is lost.
  }
}

export function EditorScreen({ g, onRun }: { g: GraphDocument; onRun?: (runId: string) => void }) {
  const { t } = useI18n()
  const { doc } = g
  // "New graph" first offers Blank or a template (F2.12); setup creates its pick directly.
  const [picker, setPicker] = useState(false)
  const newGraph = () => setPicker(true)

  // Ctrl/Cmd+S saves from anywhere on the screen; inside the editor CodeMirror has already handled it.
  // Not while a modal dialog (command palette, template picker) is open over the editor.
  useEffect(() => {
    if (!doc) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || !(e.ctrlKey || e.metaKey) || e.shiftKey || e.key.toLowerCase() !== 's') return
      if (document.querySelector('dialog[open]')) return
      e.preventDefault()
      void g.save()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [doc, g])

  return (
    <section className={`editor ev2${doc ? ' open' : ''}`} data-testid="editor">
      <h1 className="sr-only">{doc ? doc.label : t('editor.title')}</h1>
      {doc ? <Open key={doc.grant} g={g} doc={doc} onRun={onRun} onNew={newGraph} /> : <Start g={g} onNew={newGraph} />}
      {picker && (
        <TemplatePicker
          onCancel={() => setPicker(false)}
          onChoose={(draft) => {
            setPicker(false)
            g.create(draft)
          }}
        />
      )}
    </section>
  )
}

/** Error and leave banners, shared by the open editor and the start card. */
function Banners({ g }: { g: GraphDocument }) {
  const { t } = useI18n()
  return (
    <>
      {g.error && (
        <div className="v2-banner bad" role="alert" data-testid="editor-error">
          <span>{t(g.error.kind === 'open' ? 'editor.openFailed' : g.error.kind === 'edit' ? 'editor.opFailed' : g.error.kind === 'run' ? 'editor.runFailed' : 'editor.saveFailed', { message: g.error.message })}</span>
          <Button size="sm" className="ghost" onClick={g.dismissError}>
            {t('editor.dismiss')}
          </Button>
        </div>
      )}
      {g.leave && (
        <div className="v2-banner warn" role="alertdialog" aria-label={t('editor.leave')} data-testid="leave-prompt">
          <span>{t('editor.leave')}</span>
          <span className="row">
            <Button size="sm" variant="primary" onClick={() => void g.saveAndLeave()} data-testid="leave-save">
              {t('editor.leave.save')}
            </Button>
            <Button size="sm" variant="danger" onClick={() => void g.discardAndLeave()} data-testid="leave-discard">
              {t('editor.leave.discard')}
            </Button>
            <Button size="sm" onClick={g.cancelLeave} data-testid="leave-cancel">
              {t('editor.leave.cancel')}
            </Button>
          </span>
        </div>
      )}
    </>
  )
}

/** No graph open: open one, start one, or pick a recent one. */
function Start({ g, onNew }: { g: GraphDocument; onNew: () => void }) {
  const { t } = useI18n()
  return (
    <div className="ev2-start">
      <Banners g={g} />
      <div className="er-ecard ev2-start-card" data-testid="editor-empty">
        <div className="er-ecard-head">
          <h2>{t('editor.title')}</h2>
          <p>{t('editor.empty')}</p>
        </div>
        <div className="row">
          <button type="button" className="btn pr" onClick={g.open} disabled={g.busy === 'opening'} data-testid="editor-open">
            <Glyph name="folder" />
            {g.busy === 'opening' ? t('editor.opening') : t('editor.open')} <kbd>{MOD}O</kbd>
          </button>
          <button type="button" className="btn" onClick={onNew} data-testid="editor-new">
            <Glyph name="plus" />
            {t('editor.new')}
          </button>
        </div>
        <section className="ev2-recent" data-testid="editor-recent" aria-labelledby="ev2-recent-h">
          <h3 id="ev2-recent-h">{t('editor.recent')}</h3>
          {g.recent.length === 0 && <p className="er-help">{t('editor.recentEmpty')}</p>}
          <ul>
            {g.recent.map((r) => (
              <li key={r.id}>
                <button type="button" className="ev2-recent-row" onClick={() => g.openRecent(r.id)} data-testid="recent-graph">
                  <Glyph name="file" />
                  <span className="mono">{r.label}</span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  )
}

function Open({ g, doc, onRun, onNew }: { g: GraphDocument; doc: DocState; onRun?: (runId: string) => void; onNew: () => void }) {
  const { t } = useI18n()
  const [runInputs, setRunInputs] = useState('')
  const block = runBlock(doc.analysis)
  const [view, setViewState] = useState<EditorView>(loadView)
  const setView = (v: EditorView) => {
    saveView(v)
    setViewState(v)
  }
  const [focusReq, setFocusReq] = useState<FocusReq | null>(null)
  // One selection for the canvas, the find list, the Bindings panel and the inspectors.
  const selection = useSelection(doc.analysis.definition, doc.grant)
  // "Show values from run" and the chosen run's values live as long as this document is open (F2.3 requirement 16).
  const valuePicker = useValuesPicker(doc.analysis.definition?.id, doc.analysis.version)
  const issues = issuesOf(doc.analysis)
  const errorCount = issueCounts(issues).error
  // The drawer starts open when the graph opens with errors, unless the person chose; that choice is remembered.
  const [drawer, setDrawerState] = useState(() => loadDrawer() ?? errorCount > 0)
  const setDrawer = (next: boolean | ((d: boolean) => boolean)) =>
    setDrawerState((d) => {
      const v = typeof next === 'function' ? next(d) : next
      saveDrawer(v)
      return v
    })
  const readOnly = !!doc.analysis.parseError || !!doc.analysis.readonlyReasons?.length || !doc.analysis.definition
  // Choosing an issue (or "N errors block run") takes the person to the element, and to its line when the YAML is on screen.
  const focusIssue = (i: Issue) => {
    selection.select(selectionFor(i.target))
    setFocusReq((r) => ({ target: i.target, line: i.line, nonce: (r?.nonce ?? 0) + 1 }))
  }
  const run = () => void g.saveAndRun(runInputs.split('\n').map((l) => l.trim()).filter(Boolean)).then((id) => id && onRun?.(id))

  // Ctrl/Cmd+Enter runs; Escape closes the inspector.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented) return
      if (document.querySelector('dialog[open]')) return
      if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && block === null && g.busy === null) {
        e.preventDefault()
        run()
        return
      }
      const inField = e.target instanceof HTMLElement && !!e.target.closest('input,textarea,select,[role="menu"],[role="dialog"],[role="alertdialog"],.cm-editor')
      if (e.key === 'Escape' && !inField && selection.selection) selection.select(null)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const empty = !!doc.analysis.definition && Object.keys(doc.analysis.definition.nodes).length === 0
  return (
    <div className={`er-main view-${view}`}>
      <HeaderBar
        g={g}
        doc={doc}
        view={view}
        onView={setView}
        block={block}
        errorCount={errorCount}
        onShowFirstError={() => {
          const first = firstError(issues)
          setDrawer(true)
          if (first) focusIssue(first)
        }}
        onRun={run}
        onNew={onNew}
      />
      <Banners g={g} />
      {doc.conflict && (
        <div className="v2-banner warn" role="alert" data-testid="conflict-banner">
          <span>{t(doc.conflict.kind === 'changed' ? 'editor.conflict.changed' : doc.conflict.kind === 'gone' ? 'editor.conflict.gone' : 'editor.conflict.exists')}</span>
          <span className="row">
            <Button size="sm" variant="primary" onClick={() => void g.keepMine()} data-testid="keep-mine">
              {t('editor.keepMine')}
            </Button>
            <Button size="sm" onClick={() => void g.reload()} disabled={doc.conflict.kind === 'gone'} data-testid="reload">
              {t('editor.reload')}
            </Button>
          </span>
        </div>
      )}

      <div className="er-work">
        {view !== 'yaml' && <GraphCanvas g={g} doc={doc} focusReq={focusReq} selection={selection} onYaml={() => setView('yaml')} />}
        {view !== 'visual' && (
          <section className="er-yaml" aria-label={t('editor.yaml')} data-testid="yaml-card">
            <div className="yaml-host">
              <Suspense fallback={<LoadingState />}>
                <YamlEditor value={doc.draft} onChange={g.type} onSave={() => void g.save()} onUndo={g.undo} onRedo={g.redo} label={t('editor.yamlLabel')} reveal={focusReq?.line ? { line: focusReq.line, nonce: focusReq.nonce } : undefined} />
              </Suspense>
            </div>
          </section>
        )}
        <SidePanel g={g} doc={doc} selection={selection} picker={valuePicker} focusReq={focusReq} readOnly={readOnly} runInputs={runInputs} onRunInputs={setRunInputs} />
      </div>

      <ProblemsDrawer analysis={doc.analysis} open={drawer} onFocus={focusIssue} onRetry={g.revalidate} />
      <StatusBar analysis={doc.analysis} open={drawer} onToggle={() => setDrawer((d) => !d)} version={doc.analysis.version} empty={empty} />
    </div>
  )
}
