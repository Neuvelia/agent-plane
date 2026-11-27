import { useId } from 'react'
import { reasonKey, rowText, type CapabilityReport, type CapabilityTool, type RowText } from '../capabilities'
import { formatDateTime } from '../i18n'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import type { CapabilityView } from '../welcome'

// Settings, Agents (F2.5f): the agent rows of the F2.11 capability report.
// The report carries no paths, so the design's Program and "Used by" rows and
// the path chooser are not drawn; Codex is still given through AGENTPLANE_CODEX_BIN.

const ORDER = ['codex', 'claude', 'cursor-agent', 'gemini']
const PILL: Record<string, string> = { available: 'ok', detected_no_adapter: 'bl', not_configured: 'wn', missing: 'nt', unsupported_platform: 'nt', unsupported_version: 'wn', error: 'bad' }

/** Agent rows only (adapters and CLIs), known agents in the design's order, unknown ones after them as received. */
export function agentRows(report: CapabilityReport): CapabilityTool[] {
  const agents = report.tools.filter((t) => t.kind === 'agent_adapter' || t.kind === 'agent_cli')
  const rank = (t: CapabilityTool) => (ORDER.includes(t.id) ? ORDER.indexOf(t.id) : ORDER.length)
  return agents.map((t, i) => ({ t, i })).sort((a, b) => rank(a.t) - rank(b.t) || a.i - b.i).map(({ t }) => t)
}

export function AgentsPane({ view, onCheck, onRetry }: { view: CapabilityView; onCheck: () => void; onRetry: () => void }) {
  const { t } = useI18n()
  const busy = view.state === 'loading' || (view.state === 'loaded' && view.refreshing)
  const rows = view.state === 'loaded' ? agentRows(view.report) : []
  return (
    <div className="st-body" data-testid="settings-agents">
      <h1>{t('settings.nav.agents')}</h1>
      <p className="st-lede">{t('settings.agents.lede')}</p>
      {view.state === 'loading' && (
        <p className="st-card-state mu" role="status" data-testid="agents-loading">
          {t('welcome.checking')}
        </p>
      )}
      {(view.state === 'failed' || view.state === 'disconnected') && (
        <div className="st-card-state bad" role="alert" data-testid={view.state === 'failed' ? 'agents-failed' : 'agents-disconnected'}>
          <span>{view.state === 'failed' ? t('welcome.failed', { status: view.status }) : t('welcome.disconnected')}</span>
          <Button size="sm" onClick={onRetry} data-testid="agents-retry">
            {t('state.retry')}
          </Button>
        </div>
      )}
      {view.state === 'loaded' && (
        <div className="st-agents" aria-busy={view.refreshing}>
          {view.refreshing && (
            <p className="mu st-note" role="status" data-testid="agents-refreshing">
              {t('welcome.refreshing')}
            </p>
          )}
          {rows.map((tool) => (
            <AgentCard key={`${tool.kind}:${tool.id}`} tool={tool} checkedAt={view.report.checked_at} busy={busy} onCheck={onCheck} />
          ))}
          {rows.length === 0 && <p className="mu">{t('settings.agents.none')}</p>}
        </div>
      )}
      <p className="st-note">{t('settings.agents.order')}</p>
    </div>
  )
}

function word(t: ReturnType<typeof useI18n>['t'], x: RowText['name'] | RowText['status']): string {
  return 'key' in x ? t(x.key) : x.raw
}

function AgentCard({ tool, checkedAt, busy, onCheck }: { tool: CapabilityTool; checkedAt: string; busy: boolean; onCheck: () => void }) {
  const { t, locale } = useI18n()
  const r = rowText(tool)
  const reason = tool.reason ? reasonKey(tool.reason) : undefined
  // The id comes from the engine and may hold any character: not used in an element id.
  const headId = useId()
  const available = tool.status === 'available'
  return (
    <section className="st-agent" aria-labelledby={headId} data-testid={`agent-${tool.id}`} data-status={tool.status}>
      <div className="st-agent-head">
        <b id={headId}>{word(t, r.name)}</b>
        <span className={`st-pl ${r.known ? (PILL[tool.status] ?? 'nt') : 'nt'}`}>
          <span aria-hidden="true">{r.glyph}</span>
          {word(t, r.status)}
        </span>
        {tool.version && <span className="st-tg">{t('welcome.version', { version: tool.version })}</span>}
        {tool.reason && <span className="st-tg">{t('welcome.reason', { reason: tool.reason })}</span>}
        <span className="st-sp" />
        {/* aria-disabled, not disabled, while checking: keyboard focus stays on the button. */}
        <Button size="sm" onClick={() => !busy && onCheck()} aria-disabled={busy || undefined} aria-describedby={headId} data-testid={`agent-check-${tool.id}`}>
          {busy ? t('welcome.checking') : t('welcome.check')}
        </Button>
      </div>
      <div className="st-agent-body">
        {tool.reason === 'found_on_path' ? <p className="st-found">{t(r.meaning)}</p> : <p>{t(r.meaning)}</p>}
        {reason && <p>{t(reason)}</p>}
        {available && <AvailableChecks checkedAt={formatDateTime(locale, checkedAt)} codex={tool.id === 'codex'} />}
        {r.still && <p className="st-note">{t(r.still)}</p>}
      </div>
    </section>
  )
}

// "available" means the engine's probe passed every step, so the three checks
// the design lists are facts of that status, not guesses.
function AvailableChecks({ checkedAt, codex }: { checkedAt: string; codex: boolean }) {
  const { t } = useI18n()
  return (
    <>
      <div className="st-kv">
        <span className="k">{t('settings.agents.lastChecked')}</span>
        <span className="v">{checkedAt}</span>
      </div>
      <ul className="st-checks">
        {(['settings.agents.check.exists', 'settings.agents.check.version', 'settings.agents.check.flags'] as const).map((k) => (
          <li key={k}>
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="var(--ok)" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M2 6.5 5 9.5 10 3" />
            </svg>
            <b>{t(k)}</b>
          </li>
        ))}
      </ul>
      {codex && <p className="st-note">{t('settings.agents.signIn')}</p>}
    </>
  )
}
