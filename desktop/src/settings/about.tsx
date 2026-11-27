import { useEffect, useState } from 'react'
import { listRuns } from '../api'
import { copyDisplayed } from '../editor/EdgeValueBox'
import type { CapabilityReport } from '../capabilities'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import type { CapabilityView } from '../welcome'

/** Plain-text diagnostics: versions and tool statuses only. No paths, no environment, no run content. */
export function diagnosticsText(appVersion: string, report: CapabilityReport | null): string {
  const lines = [`app ${appVersion || 'unknown'}`]
  if (report) {
    lines.push(`engine ${report.engine.version} ${report.engine.os}/${report.engine.arch}`)
    for (const tool of report.tools) lines.push(`${tool.id} ${tool.status}${tool.version ? ` ${tool.version}` : ''}${tool.reason ? ` (${tool.reason})` : ''}`)
  }
  return lines.join('\n')
}

type Copy = 'idle' | 'copied' | 'failed'

// Settings, About (F2.5e): versions from the app and the capability report, and
// the number of stored runs from the run list. Copy happens only on click.
export function AboutPane({ view }: { view: CapabilityView }) {
  const { t } = useI18n()
  const appVersion = typeof window.agentplane?.appVersion === 'string' ? window.agentplane.appVersion : ''
  const report = view.state === 'loaded' ? view.report : null
  const runs = useRunCount()
  const [copy, setCopy] = useState<Copy>('idle')
  const doCopy = () => void copyDisplayed(diagnosticsText(appVersion, report)).then((ok) => setCopy(ok ? 'copied' : 'failed'))
  const engine = report ? `${report.engine.version} · ${report.engine.os}/${report.engine.arch}` : view.state === 'loading' ? t('welcome.checking') : '—'
  return (
    <div className="st-body" data-testid="settings-about">
      <h1>{t('settings.about.title', { app: t('app.name') })}</h1>
      <p className="st-lede">{t('settings.about.local')}</p>
      <div className="st-list st-about">
        <div className="st-kv">
          <span className="k">{t('settings.about.app')}</span>
          <span className="v mono" data-testid="about-app">
            {appVersion || '—'}
          </span>
        </div>
        <div className="st-kv">
          <span className="k">{t('settings.about.engine')}</span>
          <span className="v mono" data-testid="about-engine">
            {engine}
          </span>
        </div>
        <div className="st-kv">
          <span className="k">{t('settings.about.runs')}</span>
          <span className="v" data-testid="about-runs">
            {runs ?? '—'}
          </span>
        </div>
      </div>
      <div className="st-actions">
        <Button size="sm" onClick={doCopy} data-testid="about-copy">
          {t('settings.about.copy')}
        </Button>
        <span className={copy === 'failed' ? 'st-notice err' : 'st-notice mu'} role="status" data-testid="about-copy-result">
          {copy === 'copied' ? t('settings.about.copied') : copy === 'failed' ? t('settings.about.copyFailed') : ''}
        </span>
      </div>
    </div>
  )
}

/** Stored runs, or undefined while unknown: a failed read is never shown as 0. */
function useRunCount(): number | undefined {
  const [count, setCount] = useState<number>()
  useEffect(() => {
    let alive = true
    listRuns()
      .then((runs) => alive && setCount(runs.length))
      .catch(() => undefined)
    return () => {
      alive = false
    }
  }, [])
  return count
}
