import { useCallback, useEffect, useRef, useState } from 'react'
import { checkCapabilities, getCapabilities, reasonKey, rowText, type CapabilityReport, type CapabilityResult, type CapabilityTool, type RowText } from './capabilities'
import { useConnectionReport } from './connection'
import { formatDateTime } from './i18n'
import { useI18n } from './i18n/context'
import { Button, Pill, type Tone } from './ui/primitives'

export type CapabilityView =
  | { state: 'loading' }
  | { state: 'loaded'; report: CapabilityReport; refreshing: boolean }
  | { state: 'failed'; status: number }
  | { state: 'disconnected' }

const TONES: Record<string, Tone> = { available: 'p', detected_no_adapter: 'b', not_configured: 'w', missing: 'w', unsupported_platform: 'n', unsupported_version: 'w', error: 'x' }

export interface WelcomeActions {
  onTemplate: () => void
  onOpen: () => void
  onSkip: () => void
}

export function Welcome({ onTemplate, onOpen, onSkip }: WelcomeActions) {
  const { t, locale } = useI18n()
  const [view, setView] = useState<CapabilityView>({ state: 'loading' })
  const [announce, setAnnounce] = useState('')
  const heading = useRef<HTMLHeadingElement>(null)
  const seq = useRef(0)
  useConnectionReport('welcome', view.state === 'disconnected')

  useEffect(() => {
    heading.current?.focus({ preventScroll: true })
  }, [])

  // Only the latest request may settle the view, so a slow first read cannot overwrite a newer check.
  const settle = useCallback(
    (result: CapabilityResult, mine: number) => {
      if (mine !== seq.current) return
      if (result.kind === 'ok') {
        setView({ state: 'loaded', report: result.report, refreshing: false })
        setAnnounce(t('welcome.checkedAt', { time: formatDateTime(locale, result.report.checked_at) }))
      } else if (result.kind === 'failed') setView({ state: 'failed', status: result.status })
      else setView({ state: 'disconnected' })
    },
    [t, locale],
  )

  const load = useCallback(() => {
    const mine = ++seq.current
    setView({ state: 'loading' })
    void getCapabilities().then((r) => settle(r, mine))
  }, [settle])
  useEffect(load, [load])

  const check = () => {
    const mine = ++seq.current
    setView((v) => (v.state === 'loaded' ? { ...v, refreshing: true } : { state: 'loading' }))
    setAnnounce('')
    void checkCapabilities().then((r) => settle(r, mine))
  }

  const appVersion = typeof window.agentplane?.appVersion === 'string' ? window.agentplane.appVersion : ''
  return (
    <section className="welcome" data-testid="welcome">
      <h1 ref={heading} tabIndex={-1}>
        {t('welcome.title', { app: t('app.name') })}
      </h1>
      <p className="welcome-intro">{t('welcome.intro1')}</p>
      <p className="welcome-intro mu">{t('welcome.intro2')}</p>
      <CapabilityPanel view={view} announce={announce} appVersion={appVersion} onCheck={check} onRetry={load} />
      <div className="row" role="group" aria-label={t('welcome.actions')} data-testid="welcome-actions">
        <Button variant="primary" onClick={onTemplate} data-testid="welcome-template">
          {t('welcome.template')}
        </Button>
        <Button onClick={onOpen} data-testid="welcome-open">
          {t('welcome.open')}
        </Button>
        <Button onClick={onSkip} data-testid="welcome-skip">
          {t('welcome.skip')}
        </Button>
      </div>
    </section>
  )
}

export function CapabilityPanel({ view, announce, appVersion, onCheck, onRetry }: { view: CapabilityView; announce: string; appVersion: string; onCheck: () => void; onRetry: () => void }) {
  const { t } = useI18n()
  const busy = view.state === 'loading' || (view.state === 'loaded' && view.refreshing)
  return (
    <section className="card" aria-labelledby="cap-title" data-testid="capabilities">
      <header className="card-head">
        <div className="card-titles">
          <h2 className="card-title" id="cap-title">
            {t('welcome.listTitle')}
          </h2>
        </div>
        <Button size="sm" onClick={onCheck} disabled={busy || view.state === 'disconnected'} data-testid="check-again">
          {busy ? t('welcome.checking') : t('welcome.check')}
        </Button>
      </header>
      {view.state === 'loaded' && view.refreshing && (
        <p className="mu cap-note" data-testid="cap-refreshing">
          {t('welcome.refreshing')}
        </p>
      )}
      {view.state === 'loading' && (
        <ul className="cap-list" aria-busy="true" aria-labelledby="cap-title" data-testid="cap-list">
          <li className="cap-row cap-pending">
            <span className="mu">{t('welcome.checking')}</span>
          </li>
        </ul>
      )}
      {view.state === 'loaded' && (
        <ul className="cap-list" aria-busy={view.refreshing} aria-labelledby="cap-title" data-testid="cap-list">
          <EngineRow report={view.report} appVersion={appVersion} />
          {view.report.tools.map((tool) => (
            <ToolRow key={`${tool.kind}:${tool.id}`} tool={tool} />
          ))}
        </ul>
      )}
      {(view.state === 'failed' || view.state === 'disconnected') && (
        <div className="cap-state" role="alert" data-testid={view.state === 'failed' ? 'cap-failed' : 'cap-disconnected'}>
          <span>{view.state === 'failed' ? t('welcome.failed', { status: view.status }) : t('welcome.disconnected')}</span>
          <Button size="sm" onClick={onRetry} data-testid="cap-retry">
            {t('state.retry')}
          </Button>
        </div>
      )}
      <p className="mu cap-note" role="status" aria-live="polite" data-testid="cap-announce">
        {announce}
      </p>
    </section>
  )
}

function EngineRow({ report, appVersion }: { report: CapabilityReport; appVersion: string }) {
  const { t } = useI18n()
  return (
    <li className="cap-row" data-testid="cap-engine" data-status="running">
      <div className="cap-head">
        <span className="cap-name">{t('cap.tool.engine')}</span>
        <Pill tone="p" small>
          <span aria-hidden="true">✓&nbsp;</span>
          {t('cap.engine.running')}
        </Pill>
        <span className="tag" data-testid="cap-engine-version">
          {appVersion ? t('welcome.engineVersions', { engine: report.engine.version, app: appVersion }) : t('welcome.version', { version: report.engine.version })}
        </span>
      </div>
      <p className="cap-meaning">{t('cap.meaning.engine', { os: report.engine.os, arch: report.engine.arch })}</p>
    </li>
  )
}

function word(t: ReturnType<typeof useI18n>['t'], x: RowText['name'] | RowText['status']): string {
  return 'key' in x ? t(x.key) : x.raw
}

export function ToolRow({ tool }: { tool: CapabilityTool }) {
  const { t } = useI18n()
  const r = rowText(tool)
  const reason = tool.reason ? reasonKey(tool.reason) : undefined
  return (
    <li className="cap-row" data-testid={`cap-${tool.id}`} data-status={tool.status}>
      <div className="cap-head">
        <span className="cap-name">{word(t, r.name)}</span>
        <Pill tone={r.known ? (TONES[tool.status] ?? 'n') : 'n'} small>
          <span aria-hidden="true">{r.glyph}&nbsp;</span>
          <span data-testid="cap-status">{word(t, r.status)}</span>
        </Pill>
        {tool.status === 'available' && tool.version && <span className="tag">{t('welcome.version', { version: tool.version })}</span>}
        {tool.reason && <span className="tag">{t('welcome.reason', { reason: tool.reason })}</span>}
      </div>
      <p className="cap-meaning">{t(r.meaning)}</p>
      {reason && <p className="cap-still mu">{t(reason)}</p>}
      {r.still && <p className="cap-still mu">{t(r.still)}</p>}
    </li>
  )
}
