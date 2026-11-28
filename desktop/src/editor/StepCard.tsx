import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import { createContext, memo, useContext } from 'react'
import { useI18n } from '../i18n/context'
import { cardModel, type CardInput, type CardOutput } from './cardModel'
import { ErrorMark, Glyph, KindGlyph, PlaySmall, WarnMark } from './glyphs'
import type { Badges } from './issues'
import { NODE_TYPES, makeRef } from './layout'
import type { Definition, NodeTypeDescriptor, QueryResult } from './types'
import { fieldText } from './inspector/fieldText'
import { focusForClick, focusForKey, type MenuFocus } from './Menu'

// A step card (graph editor v2): the kind above the card, the name in it, and a
// self-describing body (prompt, key settings, where each input comes from, what
// it puts out), so the graph reads without opening anything. Ports are React Flow
// handles on the row they belong to. The node objects React Flow holds are static;
// everything that changes reaches the card through this context.

export interface CanvasState {
  def?: Definition
  nodeTypes?: Record<string, NodeTypeDescriptor>
  selectedNode: string | null
  badges: Badges
  /** The port an issue pointed at, outlined until something else is selected. */
  focusPort: string | null
  readOnly: boolean
  /** While a connection is being dragged: the engine's verdict per target input. */
  reasons: Record<string, QueryResult>
  /** The step whose menu is open, for the trigger's pressed look. */
  menuFor: string | null
  openMenu: (id: string, el: HTMLElement, focus: MenuFocus) => void
}

export const CanvasCtx = createContext<CanvasState>({ selectedNode: null, badges: { nodes: {}, edges: {}, ports: {} }, focusPort: null, readOnly: true, reasons: {}, menuFor: null, openMenu: () => undefined })

export interface StepData extends Record<string, unknown> {
  id: string
  kind: string
}

const typeKey = (kind: string) => (NODE_TYPES as readonly string[]).includes(kind) ? (kind as (typeof NODE_TYPES)[number]) : null

function StepCardView({ data }: NodeProps<Node<StepData>>) {
  const { t, locale } = useI18n()
  const c = useContext(CanvasCtx)
  if (!c.def) return null
  const model = cardModel(c.def, data.id, c.nodeTypes?.[data.kind], c.badges)
  const badge = c.badges.nodes[data.id]
  const selected = c.selectedNode === data.id
  const key = typeKey(data.kind)
  const kindLabel = key ? t(`editor.type.${key}`) : data.kind
  return (
    <article className={`v2-card${selected ? ' sel' : ''}${data.kind === 'agent' ? ' agent' : ''}${model.prompt ? ' wide' : ''}`} aria-label={t(selected ? 'editor.v2.cardSelected' : 'editor.v2.card', { kind: kindLabel, id: data.id })} data-testid={`node-${data.id}`} data-kind={data.kind}>
      <span className="v2-kind">
        <KindGlyph type={data.kind} />
        {kindLabel}
      </span>
      <header className="v2-ch">
        <b title={data.id}>{data.id}</b>
        {badge && badge.errors > 0 && (
          <span className="v2-bdg e" title={t('editor.nodeErrors', { count: badge.errors })}>
            <ErrorMark size={12} />
            {badge.errors}
            <span className="sr-only">{t('editor.nodeErrors', { count: badge.errors })}</span>
          </span>
        )}
        {badge && badge.errors === 0 && badge.warnings > 0 && (
          <span className="v2-bdg w" title={t('editor.nodeWarnings', { count: badge.warnings })}>
            <WarnMark size={12} color="currentColor" />
            {badge.warnings}
            <span className="sr-only">{t('editor.nodeWarnings', { count: badge.warnings })}</span>
          </span>
        )}
        <button
          type="button"
          className={`v2-more nodrag${c.menuFor === data.id ? ' open' : ''}`}
          aria-haspopup="menu"
          aria-expanded={c.menuFor === data.id}
          aria-label={t('editor.v2.stepMenu', { id: data.id })}
          onClick={(e) => {
            e.stopPropagation()
            c.openMenu(data.id, e.currentTarget, focusForClick(e))
          }}
          onKeyDown={(e) => {
            const focus = focusForKey(e.key)
            if (!focus) return
            e.preventDefault()
            e.stopPropagation()
            c.openMenu(data.id, e.currentTarget, focus)
          }}
          data-testid={`step-menu-${data.id}`}
        >
          <Glyph name="more" />
        </button>
      </header>
      {model.prompt && (
        <p className="v2-prompt">
          <span>{model.prompt}</span>
        </p>
      )}
      <dl className="v2-f">
        {model.settings.map((s) => (
          <div className="v2-row" key={s.key}>
            <dt>{fieldText(locale, s.id, s.key).label}</dt>
            <dd>
              <span className="mono">{s.value}</span>
            </dd>
          </div>
        ))}
        {model.inputs.map((p) => (
          <InputRow key={p.name} nodeId={data.id} port={p} />
        ))}
        {model.retries !== undefined && (
          <div className="v2-row">
            <dt>{t('editor.v2.retries')}</dt>
            <dd>{t('editor.v2.retriesUpTo', { count: model.retries })}</dd>
          </div>
        )}
        {model.outputs.length > 0 && (
          <div className="v2-row v2-outs">
            <dt>{t('editor.ports.outputs')}</dt>
            <dd>
              {model.outputs.map((p) => (
                <OutputToken key={p.name} nodeId={data.id} port={p} />
              ))}
            </dd>
          </div>
        )}
      </dl>
      {selected && (
        <button type="button" className="btn v2-test nodrag" aria-disabled="true" title={t('editor.v2.soonHint')} data-testid="test-step">
          {t('editor.v2.testStep')}
          <PlaySmall />
          <span className="v2-soon">{t('nav.soon')}</span>
        </button>
      )}
    </article>
  )
}

function InputRow({ nodeId, port }: { nodeId: string; port: CardInput }) {
  const { t } = useI18n()
  const c = useContext(CanvasCtx)
  const ref = makeRef(nodeId, 'inputs', port.name)
  const verdict = c.reasons[ref]
  const refused = verdict !== undefined && !verdict.ok
  const s = port.source
  return (
    <div className={`v2-row v2-in${c.focusPort === ref ? ' focus' : ''}`}>
      <dt className="mono">
        <Handle
          type="target"
          position={Position.Left}
          id={`in:${port.name}`}
          isConnectable={!c.readOnly}
          className={`v2-port${refused ? ' ed-bad' : verdict?.ok ? ' ed-ok' : ''}`}
          title={refused ? verdict.message : undefined}
          data-testid={`handle-${ref}`}
        />
        {port.name}
      </dt>
      {s.kind === 'none' ? (
        <dd className={port.error ? 'bad' : 'mu'}>
          {port.error && <ErrorMark size={12} />}
          {t('editor.v2.notConnected')}
        </dd>
      ) : (
        <dd className={port.error ? 'bad' : undefined}>
          {port.error ? <ErrorMark size={12} /> : <Glyph name="arrowLeft" size={12} width={2.2} />}
          {s.kind === 'node' ? t('editor.v2.fromNode', { node: s.node }) : t('editor.v2.fromGraphInput', { name: s.name })}
        </dd>
      )}
    </div>
  )
}

function OutputToken({ nodeId, port }: { nodeId: string; port: CardOutput }) {
  const { t } = useI18n()
  const c = useContext(CanvasCtx)
  const ref = makeRef(nodeId, 'outputs', port.name)
  // One output per line, so each port has its own handle on the card's right edge.
  return (
    <span className="v2-out">
      <span className={`v2-tok${port.warning ? ' wn' : ''}${port.error ? ' er' : ''}${c.focusPort === ref ? ' focus' : ''}`} title={port.type}>
        {port.error ? <ErrorMark size={10} /> : port.warning ? <WarnMark size={10} /> : null}
        {port.name}
        {(port.error || port.warning) && <span className="sr-only">{port.error ? t('editor.v2.portError') : t('editor.v2.portWarning')}</span>}
      </span>
      <Handle type="source" position={Position.Right} id={`out:${port.name}`} isConnectable={!c.readOnly} className="v2-port" data-testid={`handle-${ref}`} />
    </span>
  )
}

export const StepCard = memo(StepCardView)
