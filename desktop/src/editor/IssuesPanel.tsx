import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, SegmentedControl, StateMessage } from '../ui/primitives'
import type { Analysis } from './doc'
import { ErrorMark, Glyph, OkMark, WarnMark } from './glyphs'
import { filterIssues, issueCounts, parseIssue, toIssues, type Filter, type Issue } from './issues'

// Problems (F2.4, graph editor v2): the status bar always shows the counts of the
// engine's last result; the list opens as a drawer above it, errors first, each row
// a real button that takes the person to its element. It is derived from the
// analysis and decides nothing.

/** The issues a panel shows for an analysis: one blocking issue when the text does not parse. */
export function issuesOf(a: Analysis): Issue[] {
  if (a.parseError) return [parseIssue(a.parseError, a.parseLine, a.parseColumn)]
  return toIssues(a.diagnostics, a.definition)
}

export const PROBLEMS_ID = 'er-probs'

interface DrawerProps {
  analysis: Analysis
  open: boolean
  onFocus: (issue: Issue) => void
  onRetry: () => void
}

export function ProblemsDrawer({ analysis, open, onFocus, onRetry }: DrawerProps) {
  const { t } = useI18n()
  const [filter, setFilter] = useState<Filter>('all')
  const issues = issuesOf(analysis)
  const c = issueCounts(issues)
  const shown = filterIssues(issues, filter)
  const pending = analysis.status === 'validating'
  const summary = analysis.parseError ? t('editor.parseError') : c.all === 0 ? t('editor.valid') : `${t('editor.errors', { count: c.error })}, ${t('editor.warnings', { count: c.warning })}`
  return (
    <div className={`er-drawer-wrap${open ? ' open' : ''}`} data-testid="problems-drawer">
      <section id={PROBLEMS_ID} className="er-drawer" aria-label={t('issues.title')} data-testid="problems" inert={!open}>
        <div className="issues">
          <div className="sr-only" aria-live="polite" data-testid="issues-live">
            {analysis.stale || pending ? '' : summary}
          </div>
          <div className="er-drawer-head">
            <h2>{t('issues.title')}</h2>
            {!analysis.parseError && c.all > 0 && (
              <SegmentedControl
                label={t('issues.filter')}
                value={filter}
                onChange={setFilter}
                segments={[
                  { value: 'all', label: t('issues.filter.all', { count: c.all }), testId: 'issues-filter-all' },
                  { value: 'error', label: t('issues.filter.error', { count: c.error }), testId: 'issues-filter-error' },
                  { value: 'warning', label: t('issues.filter.warning', { count: c.warning }), testId: 'issues-filter-warning' },
                ]}
              />
            )}
            {pending && (
              <span className="er-st" data-testid="validating">
                {t('editor.validating')}
              </span>
            )}
            {analysis.stale && !pending && (
              <span className="er-st" data-testid="issues-stale">
                {t('editor.stale')}
              </span>
            )}
          </div>
          {analysis.unreachable && (
            <StateMessage kind="error" testId="issues-unreachable" action={<Button size="sm" onClick={onRetry}>{t('issues.retry')}</Button>}>
              {t('issues.unreachable')}
            </StateMessage>
          )}
          {!analysis.unreachable && issues.length === 0 && (analysis.status === 'validating' && analysis.stale ? <StateMessage kind="loading">{t('editor.validating')}</StateMessage> : <StateMessage kind="empty" testId="issues-empty">{t('issues.none')}</StateMessage>)}
          {issues.length > 0 && shown.length === 0 && <div className="mu empty-line" data-testid="issues-filtered-empty">{t('issues.noneInFilter')}</div>}
          <ul className={`issue-list${analysis.stale ? ' stale' : ''}`}>
            {shown.map((i) => (
              <li key={i.key}>
                <button type="button" className={`er-is ${i.severity}`} onClick={() => onFocus(i)} data-testid="issue" data-code={i.code} data-severity={i.severity}>
                  {i.severity === 'error' ? <ErrorMark /> : <WarnMark />}
                  <span className="er-is-body">
                    <b>{i.message}</b>
                    {i.suggestion && <span className="meta">{i.suggestion}</span>}
                    <span className="meta">
                      {[i.severity === 'error' ? t('config.severity.error') : t('config.severity.warning'), i.code, i.element || t('issues.graphLevel')].join(' · ')}
                      {i.line !== undefined && i.line > 0 && <span className="mono"> · {i.column ? t('issues.lineColumn', { line: i.line, column: i.column }) : t('issues.line', { line: i.line })}</span>}
                    </span>
                  </span>
                  <span className="go">{i.target.kind === 'graph' ? t('editor.v2.goToGraph') : i.target.kind === 'edge' || i.target.kind === 'binding' ? t('editor.v2.goToConnection') : t('editor.v2.goToNode')}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      </section>
    </div>
  )
}

interface BarProps {
  analysis: Analysis
  open: boolean
  onToggle: () => void
  version?: string
  empty: boolean
}

/** The status bar: problem counts that open the drawer, and how fresh the engine's check is. */
export function StatusBar({ analysis, open, onToggle, version, empty }: BarProps) {
  const { t } = useI18n()
  const issues = issuesOf(analysis)
  const c = issueCounts(issues)
  const checkedAgo = useCheckedAgo(analysis)
  const state = analysis.unreachable ? 'lost' : analysis.status === 'validating' ? 'checking' : analysis.stale ? 'stale' : 'checked'
  return (
    <footer className="er-sb">
      {c.all === 0 && empty ? (
        <span className="er-st er-sb-ok" data-testid="analysis">
          <OkMark />
          {t('editor.v2.noProblems')}
        </span>
      ) : (
        <button type="button" className="er-sbt" aria-expanded={open} aria-controls={PROBLEMS_ID} onClick={onToggle} data-testid="problems-toggle">
          <span className="sr-only">{t('issues.title')}: </span>
          {analysis.parseError ? (
            <span className="cnt" data-testid="analysis">
              <ErrorMark />
              {t('editor.parseError')}
            </span>
          ) : c.all === 0 ? (
            <span className="cnt" data-testid="analysis">
              <OkMark />
              {t('editor.v2.noProblems')}
            </span>
          ) : (
            <span className="cnt-group" data-testid="analysis">
              {c.error > 0 && (
                <span className="cnt">
                  <ErrorMark />
                  {t('editor.errors', { count: c.error })}
                </span>
              )}
              {c.warning > 0 && (
                <span className="cnt">
                  <WarnMark />
                  {t('editor.warnings', { count: c.warning })}
                </span>
              )}
            </span>
          )}
          <span className="chev" aria-hidden="true">
            <Glyph name="chevronUp" />
          </span>
        </button>
      )}
      <span className="er-sp" />
      {version && (
        <span className="er-st mono" data-testid="graph-version">
          {t('editor.version', { version: version.replace('sha256-v0:', '').slice(0, 8) })}
        </span>
      )}
      <span className={`er-st er-check ${state}`} data-testid="engine-check">
        <span className={`dot ${state === 'checked' ? 'ok' : state === 'lost' ? 'bad' : state === 'checking' ? 'brand pulse' : ''}`} />
        {state === 'lost' ? t('editor.v2.engineLost') : state === 'checking' ? t('editor.validating') : state === 'stale' ? t('editor.stale') : checkedAgo}
      </span>
    </footer>
  )
}

/** "Checked by the engine just now / N min ago", from when the last fresh answer arrived. */
function useCheckedAgo(a: Analysis): string {
  const { t } = useI18n()
  const at = useRef(Date.now())
  const [, tick] = useState(0)
  const fresh = a.status !== 'validating' && !a.stale && !a.unreachable
  useEffect(() => {
    if (fresh) at.current = Date.now()
  }, [fresh, a.diagnostics, a.version])
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 30_000)
    return () => clearInterval(id)
  }, [])
  const minutes = Math.floor((Date.now() - at.current) / 60_000)
  return minutes < 1 ? t('editor.v2.checkedNow') : t('editor.v2.checkedAgo', { count: minutes })
}
