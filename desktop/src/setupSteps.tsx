import { useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { useI18n } from './i18n/context'
import type { MessageKey } from './i18n'
import { CLI_COMMAND, type SetupCard } from './setup'
import { Lock, Monitor, NodeGlyph, Prompt, StatusGlyph, Tick } from './setupIcons'

// Steps 1, 3 and 4 of setup (canvas "Setup, revised"). Each is a headline, one
// animated picture and, at most, one quiet action; the footer owns the primary.

type HeadRef = RefObject<HTMLHeadingElement | null>
const delay = (s: number): CSSProperties => ({ animationDelay: `${s}s` })

export function StepHead({ n, optional = false, title, sub, headRef }: { n: number; optional?: boolean; title: string; sub?: string; headRef: HeadRef }) {
  const { t } = useI18n()
  return (
    <div className="su-a">
      <div className="su-eye" aria-hidden="true">
        {optional ? t('setup.eyebrowOptional', { n, total: 4 }) : t('setup.eyebrow', { n, total: 4 })}
      </div>
      <h2 className="su-h" ref={headRef} tabIndex={-1} data-testid="setup-heading">
        {title}
      </h2>
      {sub && <p className="su-sub">{sub}</p>}
    </div>
  )
}

const EXAMPLE: { kind: string; label: MessageKey; id: string; pop: number; glow: number }[] = [
  { kind: 'shell', label: 'setup.kind.shell', id: 'read-issue', pop: 0.15, glow: 1.75 },
  { kind: 'agent', label: 'setup.kind.agent', id: 'fix', pop: 0.45, glow: 3.06 },
  { kind: 'gate', label: 'setup.kind.gate', id: 'tests', pop: 0.75, glow: 4.3 },
  { kind: 'human', label: 'setup.kind.human', id: 'review', pop: 1.05, glow: 5.6 },
]

/** Step 1: the four node kinds pop in as a graph and a pulse flows through them. */
export function WelcomeStep({ headRef }: { headRef: HeadRef }) {
  const { t } = useI18n()
  return (
    <div className="su-col" data-testid="setup-step-welcome">
      <StepHead n={1} title={t('welcome.title', { app: t('app.name') })} sub={t('setup.welcome.sub')} headRef={headRef} />
      <div className="su-flow" role="img" aria-label={t('setup.welcome.graph')}>
        <span className="su-pulse" aria-hidden="true" />
        {EXAMPLE.map((n, i) => (
          <FlowNode key={n.kind} node={n} first={i === 0} label={t(n.label)} />
        ))}
      </div>
      <ul className="su-chips">
        <li className="su-chip su-a" style={delay(1.4)}>
          <Tick size={16} width={2.2} />
          {t('setup.welcome.checked')}
        </li>
        <li className="su-chip su-a" style={delay(1.55)}>
          <Monitor />
          {t('setup.welcome.local')}
        </li>
        <li className="su-chip su-a" style={delay(1.7)}>
          <Lock />
          {t('setup.welcome.private')}
        </li>
      </ul>
    </div>
  )
}

function FlowNode({ node, first, label }: { node: (typeof EXAMPLE)[number]; first: boolean; label: string }) {
  const pop = `su-pop .5s var(--su-ease) ${node.pop}s both, su-glow 4.8s ${node.glow}s infinite`
  return (
    <>
      {!first && (
        <div className="su-link" aria-hidden="true">
          <i style={delay(node.pop)} />
        </div>
      )}
      <div className="su-node" style={{ animation: pop }} aria-hidden="true">
        <span className="glyph">
          <NodeGlyph kind={node.kind} size={20} />
        </span>
        <b>{label}</b>
        <span className="id">{node.id}</span>
      </div>
    </>
  )
}

/** Step 3: Desktop and CLI lanes, then the command types itself into a terminal. */
export function AgentsStep({ headRef }: { headRef: HeadRef }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState<'' | 'ok' | 'failed'>('')
  const copy = () => {
    // The clipboard is written only inside the click (as the edge inspector's Copy).
    void Promise.resolve()
      .then(() => navigator.clipboard.writeText(CLI_COMMAND))
      .then(
        () => setCopied('ok'),
        () => setCopied('failed'),
      )
  }
  const chars = CLI_COMMAND.length
  return (
    <div className="su-col" data-testid="setup-step-agents">
      <StepHead n={3} optional title={t('setup.agents.title')} headRef={headRef} />
      <div className="su-lanes">
        <Lane title={t('setup.agents.desktop')} icon={<Monitor />} kinds={['shell', 'gate', 'human']} at={0.15} />
        <Lane title={t('setup.agents.cli')} icon={<Prompt />} kinds={['agent']} at={0.35} />
      </div>
      <div className="su-term su-a" style={delay(0.6)}>
        <div className="tb">
          <i />
          <i />
          <i />
          <span className="st" role="status" aria-live="polite" data-testid="setup-copied">
            {copied === 'ok' ? t('editor.edgeValue.copied') : copied === 'failed' ? t('editor.edgeValue.copyFailed') : ''}
          </span>
          <button type="button" className="su-btn" onClick={copy} aria-label={t('setup.agents.copyLabel')} data-testid="setup-copy">
            {t('setup.agents.copy')}
          </button>
        </div>
        <pre>
          <span className="pm" aria-hidden="true">
            $
          </span>
          <code className="su-typed" style={{ width: `${chars}ch`, '--su-chars': chars } as CSSProperties} data-testid="setup-command">
            {CLI_COMMAND}
          </code>
          <span className="su-caret" aria-hidden="true" />
        </pre>
      </div>
      <p className="su-soon su-a" style={delay(2.8)}>
        {t('setup.agents.soon')}
        <span className="c">{t('cap.tool.claude')}</span>
        <span className="c">{t('cap.tool.cursor-agent')}</span>
        <span className="c">{t('cap.tool.gemini')}</span>
      </p>
    </div>
  )
}

function Lane({ title, icon, kinds, at }: { title: string; icon: ReactNode; kinds: string[]; at: number }) {
  const { t } = useI18n()
  return (
    <div className="su-lane su-a" style={delay(at)}>
      <span className="lt">
        {icon}
        {title}
      </span>
      <ul className="ns">
        {kinds.map((k) => (
          <li key={k} className="su-mini" title={t(`setup.kind.${k}` as MessageKey)}>
            <NodeGlyph kind={k} />
            <span className="sr-only">{t(`setup.kind.${k}` as MessageKey)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

/** Step 4: template cards that show their steps as icons; the picked one plays its flow. */
export function GraphStep({ headRef, cards, choice, onChoice, onOpen }: { headRef: HeadRef; cards: SetupCard[]; choice: string; onChoice: (id: string) => void; onOpen: () => void }) {
  const { t } = useI18n()
  return (
    <div className="su-col" data-testid="setup-step-graph">
      <StepHead n={4} title={t('setup.graph.title')} headRef={headRef} />
      <fieldset className="su-tpls">
        <legend className="sr-only">{t('picker.legend')}</legend>
        {cards.map((c, i) => (
          <TemplateCard key={c.id} card={c} on={c.id === choice} at={0.15 + i * 0.12} onPick={() => onChoice(c.id)} />
        ))}
      </fieldset>
      <button type="button" className="su-btn ghost su-a" style={delay(0.7)} onClick={onOpen} data-testid="welcome-open">
        {t('setup.graph.open')}
      </button>
    </div>
  )
}

function TemplateCard({ card, on, at, onPick }: { card: SetupCard; on: boolean; at: number; onPick: () => void }) {
  const { t } = useI18n()
  const blank = card.flow.length === 0
  const warn = card.placeholders > 0 || card.agent
  const metaId = `su-tpl-${card.id}-meta`
  return (
    <label className={`su-tpl su-a${on ? ' on' : ''}`} style={delay(at)} data-testid={`setup-tpl-${card.id}`}>
      <span className="hd">
        <input type="radio" name="setup-template" value={card.id} checked={on} onChange={onPick} aria-describedby={metaId} data-testid={`setup-template-${card.id}`} />
        {/* Template ids are file names and stay verbatim; Blank has no file, so it gets a translated name. */}
        <span className={blank ? 'nm word' : 'nm'}>{blank ? t('picker.blank.title') : card.id}</span>
        {card.recommended && <span className="su-rec">{t('setup.graph.recommended')}</span>}
      </span>
      <span className="su-path" role="img" aria-label={blank ? t('setup.graph.emptyGraph') : t('setup.graph.steps', { types: card.flow.map((k) => t(`setup.kind.${k}` as MessageKey)).join(', ') })}>
        {blank ? (
          <span className="blank">{t('setup.graph.empty')}</span>
        ) : (
          <span className="track">
            {card.flow.map((k, i) => (
              <span key={i} style={{ display: 'contents' }}>
                {i > 0 && <span className="seg" />}
                <span className="su-mini">
                  <NodeGlyph kind={k} />
                </span>
              </span>
            ))}
            {on && <span className="pulse" aria-hidden="true" />}
          </span>
        )}
      </span>
      <span className="su-meta" id={metaId}>
        {!blank && (warn ? <StatusGlyph tone="warn" size={14} /> : <Tick size={14} color="var(--ok)" />)}
        <span>{blank ? t('setup.graph.blankMeta') : card.placeholders > 0 ? t('setup.graph.fill', { count: card.placeholders }) : t('setup.graph.runsAsIs')}</span>
        {card.agent && <span>· {t('setup.graph.agentCli')}</span>}
      </span>
    </label>
  )
}
