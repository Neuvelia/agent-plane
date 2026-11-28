import type { CSSProperties, ReactNode } from 'react'
import { reasonKey, rowText, type CapabilityReport, type CapabilityTool, type RowText } from './capabilities'
import { useI18n } from './i18n/context'
import { rowTone, scanDelay, verdictDelay, verdictOf, type CheckState, type RowTone } from './setup'
import { StatusGlyph, Tick } from './setupIcons'

// Step 2 of setup: the engine's capability report (F2.11) as a scan. Rows land
// one by one (spinner to glyph), then the verdict. Every row keeps its status
// word, meaning and guidance; the order and wording come from the report.

export type CapabilityView = CheckState

type T = ReturnType<typeof useI18n>['t']
const TONE_TEXT: Record<RowTone, string> = { ok: 'su-ok', warn: 'su-wn', off: 'su-mu', bad: 'su-bd' }
const word = (t: T, x: RowText['name'] | RowText['status']) => ('key' in x ? t(x.key) : x.raw)

export function CapabilityPanel({ view, appVersion, onCheck, onRetry }: { view: CapabilityView; appVersion: string; onCheck: () => void; onRetry: () => void }) {
  const { t } = useI18n()
  return (
    <section className="su-check" aria-label={t('welcome.listTitle')} data-testid="capabilities">
      {view.state === 'loading' && <Checking />}
      {view.state === 'loaded' && <Report report={view.report} refreshing={view.refreshing} appVersion={appVersion} onCheck={onCheck} />}
      {(view.state === 'failed' || view.state === 'disconnected') && <Unavailable view={view} onRetry={onRetry} />}
    </section>
  )
}

function Verdict({ view, delay }: { view: CapabilityView; delay?: number }) {
  const { t } = useI18n()
  const v = verdictOf(view)
  const style = delay === undefined ? undefined : { animationDelay: `${delay}s` }
  return (
    <div className={`su-verdict ${v.tone === 'ok' ? '' : v.tone}${delay === undefined ? '' : ' su-a'}`} role={v.tone === 'bad' ? 'alert' : undefined} style={style} data-testid="setup-verdict" data-tone={v.tone}>
      {v.tone === 'busy' && (
        <span className="sp" aria-hidden="true">
          <span className="su-spin" />
        </span>
      )}
      {v.tone === 'ok' && <Tick size={18} />}
      {v.tone === 'warn' && <StatusGlyph tone="warn" size={18} />}
      {v.tone === 'bad' && <StatusGlyph tone="bad" size={18} />}
      {t(v.key, view.state === 'failed' ? { status: view.status } : undefined)}
    </div>
  )
}

function Checking() {
  const { t } = useI18n()
  const rows = [t('cap.tool.engine'), t('cap.tool.git'), t('setup.check.agentClis')]
  return (
    <>
      <Verdict view={{ state: 'loading' }} />
      <ul className="su-scan" aria-busy="true" aria-label={t('welcome.checking')} data-testid="cap-list">
        {rows.map((name, i) => (
          <li key={name} className="su-row">
            <span className="ic" aria-hidden="true">
              <span className="su-spin" style={{ opacity: 1, animation: `su-rot .7s linear ${i / 10}s infinite` }} />
            </span>
            <span className="nm">
              <b>{name}</b>
            </span>
            <span className="stt su-mu">{t('welcome.checking')}</span>
          </li>
        ))}
      </ul>
    </>
  )
}

function Report({ report, refreshing, appVersion, onCheck }: { report: CapabilityReport; refreshing: boolean; appVersion: string; onCheck: () => void }) {
  const { t } = useI18n()
  const count = report.tools.length + 1
  return (
    <>
      {refreshing ? <Verdict view={{ state: 'loaded', report, refreshing }} /> : <Verdict view={{ state: 'loaded', report, refreshing }} delay={verdictDelay(count)} />}
      {refreshing && (
        <p className="su-note" data-testid="cap-refreshing">
          {t('welcome.refreshing')}
        </p>
      )}
      {/* A new report remounts the list, so a finished re-check scans again. */}
      <ul key={report.checked_at} className="su-scan" aria-busy={refreshing} aria-label={t('welcome.listTitle')} data-testid="cap-list">
        <EngineRow report={report} appVersion={appVersion} rows={count} />
        {report.tools.map((tool, i) => (
          <ToolRow key={`${tool.kind}:${tool.id}`} tool={tool} index={i + 1} rows={count} />
        ))}
      </ul>
      <button type="button" className="su-btn ghost su-a" onClick={onCheck} disabled={refreshing} data-testid="check-again">
        {refreshing ? t('welcome.checking') : t('welcome.check')}
      </button>
    </>
  )
}

/** Row timing: the row pops in, its spinner turns until the glyph and status word land. */
function timing(index: number, rows: number) {
  const d = scanDelay(index, rows)
  return {
    row: { animationDelay: `${d.row}s` } as CSSProperties,
    spin: { animation: `su-rot .7s linear infinite, su-gone ${(d.settle - d.row).toFixed(2)}s ${d.row}s both` } as CSSProperties,
    land: { animationDelay: `${d.settle}s` } as CSSProperties,
  }
}

function Row({ index, rows, tone, testId, status, name, tag, tagTestId, statusWord, children }: { index: number; rows: number; tone: RowTone; testId: string; status: string; name: string; tag?: string; tagTestId?: string; statusWord: string; children: ReactNode }) {
  const at = timing(index, rows)
  return (
    <li className="su-row cap-row su-a" style={at.row} data-testid={testId} data-status={status}>
      <span className="ic" aria-hidden="true">
        <span className="su-spin" style={at.spin} />
        <span className="su-a" style={at.land}>
          <StatusGlyph tone={tone} />
        </span>
      </span>
      <span className="nm">
        <b>{name}</b>
        {tag && (
          <span className="su-tag" data-testid={tagTestId}>
            {tag}
          </span>
        )}
      </span>
      <span className={`stt su-a ${TONE_TEXT[tone]}`} style={at.land} data-testid="cap-status">
        {statusWord}
      </span>
      <span className="more">{children}</span>
    </li>
  )
}

function EngineRow({ report, appVersion, rows }: { report: CapabilityReport; appVersion: string; rows: number }) {
  const { t } = useI18n()
  const tag = appVersion ? t('welcome.engineVersions', { engine: report.engine.version, app: appVersion }) : t('welcome.version', { version: report.engine.version })
  return (
    <Row index={0} rows={rows} tone="ok" testId="cap-engine" status="running" name={t('cap.tool.engine')} tag={tag} tagTestId="cap-engine-version" statusWord={t('cap.engine.running')}>
      <span>{t('cap.meaning.engine', { os: report.engine.os, arch: report.engine.arch })}</span>
    </Row>
  )
}

function ToolRow({ tool, index, rows }: { tool: CapabilityTool; index: number; rows: number }) {
  const { t } = useI18n()
  const r = rowText(tool)
  const reason = tool.reason ? reasonKey(tool.reason) : undefined
  const tag = tool.status === 'available' && tool.version ? tool.version : tool.reason ? t('welcome.reason', { reason: tool.reason }) : undefined
  return (
    <Row index={index} rows={rows} tone={r.known ? rowTone(tool) : 'off'} testId={`cap-${tool.id}`} status={tool.status} name={word(t, r.name)} tag={tag} statusWord={word(t, r.status)}>
      <span>{t(r.meaning)}</span>
      {reason && <span>{t(reason)}</span>}
      {r.still && <span>{t(r.still)}</span>}
    </Row>
  )
}

function Unavailable({ view, onRetry }: { view: Extract<CapabilityView, { state: 'failed' } | { state: 'disconnected' }>; onRetry: () => void }) {
  const { t } = useI18n()
  return (
    <div className="su-check" data-testid={view.state === 'failed' ? 'cap-failed' : 'cap-disconnected'}>
      <Verdict view={view} />
      {view.state === 'disconnected' && (
        <ul className="su-scan" aria-label={t('welcome.listTitle')}>
          <li className="su-row">
            <span className="ic" aria-hidden="true">
              <StatusGlyph tone="bad" />
            </span>
            <span className="nm">
              <b>{t('cap.tool.engine')}</b>
            </span>
            <span className="stt su-bd">{t('setup.check.notConnected')}</span>
            <span className="more">{t('welcome.disconnected')}</span>
          </li>
          <li className="su-row">
            <span className="ic" aria-hidden="true">
              <StatusGlyph tone="off" />
            </span>
            <span className="nm">
              <b>{t('setup.check.others')}</b>
            </span>
            <span className="stt su-mu">{t('setup.check.notChecked')}</span>
          </li>
        </ul>
      )}
      <button type="button" className="su-btn pr" onClick={onRetry} data-testid="cap-retry">
        {t('state.retry')}
      </button>
    </div>
  )
}
