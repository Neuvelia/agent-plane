import { Suspense, lazy, useEffect, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Card, HelpTip, ListRow, Pill, SegmentedControl, Tag } from '../ui/primitives'
import { GraphCanvas } from './GraphCanvas'
import { IssuesPanel, issuesOf } from './IssuesPanel'
import { firstError, issueCounts, type FocusReq, type Issue } from './issues'
import { isDirty, runBlock, type DocState } from './doc'
import { selectionFor, useSelection } from './selection'
import type { GraphDocument } from './useGraphDocument'
import { LoadingState } from '../states'
import { useValuesPicker } from '../useRunValues'
import { TemplatePicker } from './TemplatePicker'

// CodeMirror is loaded when the editor is first shown, not at application start.
const YamlEditor = lazy(() => import('./YamlEditor').then((m) => ({ default: m.YamlEditor })))

export function EditorScreen({ g, onRun }: { g: GraphDocument; onRun?: (runId: string) => void }) {
  const { t } = useI18n()
  const { doc } = g
  // "New graph" first offers Blank or a template (F2.12); setup creates its pick directly.
  const [picker, setPicker] = useState(false)
  const newGraph = () => setPicker(true)

  // Ctrl/Cmd+S saves from anywhere on the screen; inside the editor CodeMirror has already handled it.
  useEffect(() => {
    if (!doc) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || !(e.ctrlKey || e.metaKey) || e.shiftKey || e.key.toLowerCase() !== 's') return
      e.preventDefault()
      void g.save()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [doc, g])

  return (
    <section className="editor" data-testid="editor">
      <h1>{doc ? doc.label : t('editor.title')}</h1>
      {g.error && (
        <div className="banner" role="alert" data-testid="editor-error">
          <span>{t(g.error.kind === 'open' ? 'editor.openFailed' : g.error.kind === 'edit' ? 'editor.opFailed' : g.error.kind === 'run' ? 'editor.runFailed' : 'editor.saveFailed', { message: g.error.message })}</span>
          <Button size="sm" onClick={g.dismissError}>
            {t('editor.dismiss')}
          </Button>
        </div>
      )}
      {g.leave && (
        <div className="banner warn" role="alertdialog" aria-label={t('editor.leave')} data-testid="leave-prompt">
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
      {doc ? <Open key={doc.grant} g={g} doc={doc} onRun={onRun} onNew={newGraph} /> : <Empty g={g} onNew={newGraph} />}
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

function Empty({ g, onNew }: { g: GraphDocument; onNew: () => void }) {
  const { t } = useI18n()
  return (
    <>
      <Card title={t('editor.title')} testId="editor-empty">
        <p className="mu empty-line">{t('editor.empty')}</p>
        <div className="row" style={{ padding: '0 var(--space-12) var(--space-8)' }}>
          <Button variant="primary" onClick={g.open} disabled={g.busy === 'opening'} data-testid="editor-open">
            {g.busy === 'opening' ? t('editor.opening') : t('editor.open')}
          </Button>
          <Button onClick={onNew} data-testid="editor-new">
            {t('editor.new')}
          </Button>
        </div>
      </Card>
      <Card title={t('editor.recent')} testId="editor-recent">
        {g.recent.length === 0 && <div className="mu empty-line">{t('editor.recentEmpty')}</div>}
        {g.recent.map((r) => (
          <ListRow key={r.id} onClick={() => g.openRecent(r.id)} testId="recent-graph">
            <span style={{ flex: 1 }} className="mono">
              {r.label}
            </span>
          </ListRow>
        ))}
      </Card>
    </>
  )
}

function Open({ g, doc, onRun, onNew }: { g: GraphDocument; doc: DocState; onRun?: (runId: string) => void; onNew: () => void }) {
  const { t } = useI18n()
  const dirty = isDirty(doc)
  const saving = g.busy === 'saving'
  const [runInputs, setRunInputs] = useState('')
  const block = runBlock(doc.analysis)
  const hasInputs = Object.keys(doc.analysis.definition?.inputs ?? {}).length > 0
  const [view, setView] = useState<'visual' | 'yaml' | 'split'>('split')
  const [focusReq, setFocusReq] = useState<FocusReq | null>(null)
  // One selection for the canvas, the node list, the Bindings panel and the inspectors.
  const selection = useSelection(doc.analysis.definition, doc.grant)
  // "Show values from run" and the chosen run's values live as long as this document is open (F2.3 requirement 16).
  const valuePicker = useValuesPicker(doc.analysis.definition?.id, doc.analysis.version)
  const issues = issuesOf(doc.analysis)
  const errorCount = issueCounts(issues).error
  // Choosing an issue (or "show first error") takes the person to the element, and to its line when the YAML is on screen.
  const focusIssue = (i: Issue) => {
    selection.select(selectionFor(i.target))
    setFocusReq((r) => ({ target: i.target, line: i.line, nonce: (r?.nonce ?? 0) + 1 }))
  }
  return (
    <>
      <div className="row between">
        <div className="row">
          {doc.isNew ? <Pill tone="w" testId="doc-state">{t('editor.notCreated')}</Pill> : dirty ? <Pill tone="w" testId="doc-state">{t('editor.unsaved')}</Pill> : <Pill tone="p" testId="doc-state">{t('editor.saved')}</Pill>}
          {doc.analysis.version && <Tag>{t('editor.version', { version: doc.analysis.version.replace('sha256-v0:', '').slice(0, 8) })}</Tag>}
        </div>
        <div className="row" role="toolbar" aria-label={t('editor.toolbar')}>
          <Button size="sm" onClick={g.undo} disabled={doc.past.length === 0} data-testid="editor-undo">
            {t('editor.undo')}
          </Button>
          <Button size="sm" onClick={g.redo} disabled={doc.future.length === 0} data-testid="editor-redo">
            {t('editor.redo')}
          </Button>
          <Button size="sm" variant="primary" onClick={() => void g.save()} disabled={saving || (!dirty && !doc.isNew)} data-testid="editor-save">
            {saving ? t('editor.saving') : t('editor.save')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            disabled={block !== null || g.busy !== null}
            title={block ? t(`editor.runBlocked.${block}`, { count: errorCount }) : undefined}
            onClick={() => void g.saveAndRun(runInputs.split('\n').map((l) => l.trim()).filter(Boolean)).then((id) => id && onRun?.(id))}
            data-testid="editor-save-run"
          >
            {g.busy === 'starting' ? t('editor.starting') : t('editor.saveRun')}
          </Button>
          <Button size="sm" onClick={g.open} data-testid="editor-open">
            {t('editor.open')}
          </Button>
          <Button size="sm" onClick={onNew} data-testid="editor-new">
            {t('editor.new')}
          </Button>
          <Button size="sm" onClick={g.close} data-testid="editor-close">
            {t('editor.close')}
          </Button>
        </div>
      </div>

      {block && (
        <div className="row mu" data-testid="run-blocked">
          <span>{t(`editor.runBlocked.${block}`, { count: errorCount })}</span>
          {block === 'errors' && firstError(issues) && (
            <Button size="sm" onClick={() => focusIssue(firstError(issues)!)} data-testid="run-blocked-focus">
              {t('editor.runBlocked.showFirst')}
            </Button>
          )}
        </div>
      )}
      {hasInputs && (
        <div className="ed-inputs">
          <label htmlFor="run-inputs">{t('editor.runInputs')}</label>
          <textarea id="run-inputs" value={runInputs} onChange={(e) => setRunInputs(e.target.value)} placeholder="name=value" data-testid="run-inputs" />
        </div>
      )}

      {doc.conflict && (
        <div className="banner warn" role="alert" data-testid="conflict-banner">
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

      <div className="row between">
        <SegmentedControl
          label={t('editor.view')}
          value={view}
          onChange={setView}
          segments={(['visual', 'yaml', 'split'] as const).map((v) => ({ value: v, label: t(`editor.view.${v}`), testId: `view-${v}` }))}
        />
      </div>

      <div className={`ed-views ${view}`}>
        {view !== 'yaml' && <GraphCanvas g={g} doc={doc} focusReq={focusReq} selection={selection} picker={valuePicker} />}
        {view !== 'visual' && (
          <Card title={t('editor.yaml')} action={<HelpTip>{t('editor.yamlHelp')}</HelpTip>} testId="yaml-card">
            <div className="yaml-host">
              <Suspense fallback={<LoadingState />}>
                <YamlEditor value={doc.draft} onChange={g.type} onSave={() => void g.save()} onUndo={g.undo} onRedo={g.redo} label={t('editor.yamlLabel')} reveal={focusReq?.line ? { line: focusReq.line, nonce: focusReq.nonce } : undefined} />
              </Suspense>
            </div>
          </Card>
        )}
      </div>

      <IssuesPanel analysis={doc.analysis} onFocus={focusIssue} onRetry={g.revalidate} />
    </>
  )
}
