import { useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Card, HelpTip, Pill, SegmentedControl, Tag } from '../ui/primitives'
import { StateMessage } from '../ui/primitives'
import type { Analysis } from './doc'
import { filterIssues, issueCounts, parseIssue, toIssues, type Filter, type Issue } from './issues'

// The Issues panel (F2.4): the engine's diagnostics of the last result, ordered
// and filterable, each a real button that takes the person to its element. It is
// derived from the analysis and decides nothing.

/** The issues a panel shows for an analysis: one blocking issue when the text does not parse. */
export function issuesOf(a: Analysis): Issue[] {
  if (a.parseError) return [parseIssue(a.parseError, a.parseLine)]
  return toIssues(a.diagnostics, a.definition)
}

export function IssuesPanel({ analysis, onFocus, onRetry }: { analysis: Analysis; onFocus: (issue: Issue) => void; onRetry: () => void }) {
  const { t } = useI18n()
  const [filter, setFilter] = useState<Filter>('all')
  const issues = issuesOf(analysis)
  const c = issueCounts(issues)
  const shown = filterIssues(issues, filter)
  const pending = analysis.status === 'validating'
  const summary = analysis.parseError ? t('editor.parseError') : c.all === 0 ? t('editor.valid') : `${t('editor.errors', { count: c.error })}, ${t('editor.warnings', { count: c.warning })}`

  return (
    <Card
      title={t('issues.title')}
      testId="problems"
      action={
        <span className="row">
          {pending && <span className="mu" data-testid="validating">{t('editor.validating')}</span>}
          {analysis.stale && !pending && <span className="mu" data-testid="issues-stale">{t('editor.stale')}</span>}
          {analysis.parseError ? <Pill tone="x" testId="analysis">{t('editor.parseError')}</Pill> : c.error > 0 ? <Pill tone="x" testId="analysis">{t('editor.errors', { count: c.error })}</Pill> : c.warning > 0 ? <Pill tone="w" testId="analysis">{t('editor.warnings', { count: c.warning })}</Pill> : <Pill tone="p" testId="analysis">{t('editor.valid')}</Pill>}
          <HelpTip>{t('issues.help')}</HelpTip>
        </span>
      }
    >
      <div role="region" aria-label={t('issues.title')} className="issues">
        <div className="sr-only" aria-live="polite" data-testid="issues-live">
          {analysis.stale || pending ? '' : summary}
        </div>
        {analysis.unreachable && (
          <StateMessage kind="error" testId="issues-unreachable" action={<Button size="sm" onClick={onRetry}>{t('issues.retry')}</Button>}>
            {t('issues.unreachable')}
          </StateMessage>
        )}
        {!analysis.parseError && (
          <div className="issues-filter">
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
          </div>
        )}
        {!analysis.unreachable && issues.length === 0 && (analysis.status === 'validating' && analysis.stale ? <StateMessage kind="loading">{t('editor.validating')}</StateMessage> : <StateMessage kind="empty" testId="issues-empty">{t('issues.none')}</StateMessage>)}
        {issues.length > 0 && shown.length === 0 && <div className="mu empty-line" data-testid="issues-filtered-empty">{t('issues.noneInFilter')}</div>}
        <ul className={`issue-list${analysis.stale ? ' stale' : ''}`}>
          {shown.map((i) => (
            <li key={i.key}>
              <button type="button" className={`issue ${i.severity}`} onClick={() => onFocus(i)} data-testid="issue" data-code={i.code} data-severity={i.severity}>
                <Pill tone={i.severity === 'error' ? 'x' : 'w'} small>
                  {i.severity === 'error' ? t('config.severity.error') : t('config.severity.warning')}
                </Pill>
                <span className="issue-body">
                  <span>
                    <b className="mono">{i.code}</b> {i.message}
                  </span>
                  {i.suggestion && <span className="mu">{i.suggestion}</span>}
                  <span className="row" style={{ gap: 6 }}>
                    <Tag>{i.element || t('issues.graphLevel')}</Tag>
                    {i.line !== undefined && i.line > 0 && <span className="mu mono">{t('issues.line', { line: i.line })}</span>}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </Card>
  )
}
