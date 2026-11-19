import { useI18n } from './i18n/context'
import type { MessageKey } from './i18n'
import type { DataPort, NodeData, PortState } from './dataModel'
import type { RecordedValue } from './values'
import { Tag } from './ui/primitives'

// The Data tab (F2.7 FR12-14): declared inputs with the recorded value of their
// source, and declared outputs with the accepted value. Every value is text: no
// HTML, no link, no path opened. Artifacts are references only.

/** Values over this many characters start collapsed (FR12). */
export const COLLAPSE_OVER = 2048

const utf8Length = (text: string): number => new TextEncoder().encode(text).length

/** json pretty-printed when it parses; text as received when it does not. */
export function prettyJson(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

function Marks({ v }: { v: RecordedValue }) {
  const { t } = useI18n()
  const carried = v.value === undefined ? 0 : utf8Length(v.value)
  return (
    <span className="mu" data-testid="value-marks">
      {t('data.size', { bytes: v.sizeBytes })}
      {v.truncated && ` · ${v.value === undefined ? t('data.truncatedNone') : t('data.truncated', { carried, total: v.sizeBytes })}`}
      {v.redacted && ` · ${t('data.redacted')}`}
    </span>
  )
}

function ValueText({ v }: { v: RecordedValue }) {
  const { t } = useI18n()
  const text = v.value ?? ''
  if (v.type === 'artifact_ref')
    return (
      <div>
        <span className="mono" data-testid="value-text">{text}</span>
        <div className="mu">{t('data.artifactNote')}</div>
      </div>
    )
  if (text === '') return <span className="mu" data-testid="value-empty">{t('data.empty')}</span>
  const shown = v.type === 'json' ? prettyJson(text) : text
  // A native <details> is the collapsible: keyboard operable, text only inside.
  if (v.type === 'json' || text.length > COLLAPSE_OVER)
    return (
      <details open={text.length <= COLLAPSE_OVER} data-testid="value-details">
        <summary>{t(v.type === 'json' ? 'data.showJson' : 'data.showText')}</summary>
        <pre className="log small" data-testid="value-text">{shown}</pre>
      </details>
    )
  return <pre className="log small" data-testid="value-text">{shown}</pre>
}

const STATE_KEY: Partial<Record<PortState['kind'], MessageKey>> = {
  bindingsUnavailable: 'data.state.bindingsUnavailable',
  optionalUnbound: 'data.state.optionalUnbound',
  unbound: 'data.state.unbound',
  olderRun: 'data.state.olderRun',
  notRecorded: 'data.state.notRecorded',
  notFinished: 'data.state.notFinished',
  failedOrSkipped: 'data.state.failedOrSkipped',
  noValue: 'data.state.noValue',
}

function StateView({ state, onOpenSettings }: { state: PortState; onOpenSettings?: () => void }) {
  const { t } = useI18n()
  if (state.kind === 'recorded')
    return (
      <div data-testid="value-recorded">
        <Tag>{state.value.type}</Tag> <Marks v={state.value} />
        <ValueText v={state.value} />
      </div>
    )
  if (state.kind === 'notCarried')
    return (
      <div data-testid="value-state-notCarried">
        <span>{t('data.state.notCarried')}</span> <Tag>{state.value.type}</Tag> <Marks v={state.value} />
        <div className="mu mono">sha256 {state.value.sha256.slice(0, 12)}</div>
      </div>
    )
  if (state.kind === 'ambiguous')
    return (
      <div data-testid="value-state-ambiguous">{t('data.state.ambiguous', { first: state.sequences[0], second: state.sequences[1] })}</div>
    )
  const key = STATE_KEY[state.kind]!
  return (
    <div data-testid={`value-state-${state.kind}`}>
      {t(key)}
      {state.kind === 'notRecorded' && onOpenSettings && (
        <>
          {' '}
          <button type="button" className="link-button" onClick={onOpenSettings} data-testid="data-open-settings">
            {t('data.openSettings')}
          </button>
        </>
      )}
    </div>
  )
}

function PortRow({ port, onOpenSettings }: { port: DataPort; onOpenSettings?: () => void }) {
  const { t } = useI18n()
  return (
    <li className="data-port" data-testid={`data-port-${port.name}`}>
      <div className="row">
        <b className="mono">{port.name}</b>
        <Tag>{port.type}</Tag>
        {!port.required && <Tag>{t('data.optional')}</Tag>}
        {port.source && (
          <span className="mu">
            {t('data.from')} <span className="mono" data-testid="data-source">{port.source}</span>
          </span>
        )}
      </div>
      <StateView state={port.state} onOpenSettings={onOpenSettings} />
    </li>
  )
}

export function DataTab({ data, kind, onOpenSettings }: { data: NodeData; kind?: string; onOpenSettings?: () => void }) {
  const { t } = useI18n()
  if (data.unavailable)
    return (
      <div className="mu" data-testid="data-unavailable">
        {t('data.state.bindingsUnavailable')}
      </div>
    )
  return (
    <div data-testid="data-tab">
      <section aria-label={t('data.inputs')}>
        <div className="lb">{t('data.inputs')}</div>
        {data.inputs.length === 0 ? <div className="mu">{t('data.noneDeclared')}</div> : <ul className="data-ports">{data.inputs.map((p) => <PortRow key={p.name} port={p} onOpenSettings={onOpenSettings} />)}</ul>}
      </section>
      <section aria-label={t('data.outputs')}>
        <div className="lb">{t('data.outputs')}</div>
        {data.outputs.length === 0 ? <div className="mu">{t('data.noneDeclared')}</div> : <ul className="data-ports">{data.outputs.map((p) => <PortRow key={p.name} port={p} onOpenSettings={onOpenSettings} />)}</ul>}
      </section>
      {(kind === 'agent' || kind === 'shell') && (
        <p className="mu" data-testid="data-not-recorded-by-design">
          {t(kind === 'agent' ? 'data.rawTaskNotRecorded' : 'data.rawCommandNotRecorded')}
        </p>
      )}
    </div>
  )
}
