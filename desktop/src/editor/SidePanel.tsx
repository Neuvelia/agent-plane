import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Pill } from '../ui/primitives'
import type { ValuesPicker } from '../useRunValues'
import { BindingsPanel } from './BindingsPanel'
import { removeOp, sourceType, type Binding } from './bindings'
import type { DocState } from './doc'
import { EdgeInspector } from './EdgeInspector'
import { Glyph, KindGlyph } from './glyphs'
import { GraphInputsCard } from './GraphInputsCard'
import { GraphOutputsCard } from './GraphOutputsCard'
import { Inspector } from './inspector/Inspector'
import { badgesOf, type FocusReq } from './issues'
import { NODE_TYPES, parseRef } from './layout'
import type { Selection, SelectionOwner } from './selection'
import type { Definition, EdgeDef } from './types'
import type { GraphDocument } from './useGraphDocument'

// The inspector panel on the right of the graph editor v2. It follows the one
// selection: a step shows the node inspector (F2.2) and its connections, an edge
// or a graph input binding the edge inspector (F2.3 E5b.2), and nothing selected
// the graph itself (inputs, outputs, bindings, run inputs). The Bindings panel
// keeps its place in the tree between the edge and graph views, so choosing a row
// and coming back does not reset it.

interface Props {
  g: GraphDocument
  doc: DocState
  selection: SelectionOwner
  picker: ValuesPicker
  focusReq: FocusReq | null
  readOnly: boolean
  runInputs: string
  onRunInputs: (text: string) => void
}

type View = 'node' | 'edge' | 'graph'

export function SidePanel({ g, doc, selection, picker, focusReq, readOnly, runInputs, onRunInputs }: Props) {
  const { t } = useI18n()
  const def = doc.analysis.definition
  const sel = selection.selection
  const view: View = sel?.kind === 'node' && def?.nodes[sel.id] ? 'node' : sel && sel.kind !== 'node' && def ? 'edge' : 'graph'
  const selectedHead = useRef<HTMLHeadingElement>(null)
  const graphHead = useRef<HTMLHeadingElement>(null)
  const [focusTick, setFocusTick] = useState(0)

  // An issue was chosen (the editor already selected its element): once the panel shows it, move focus to its heading.
  useEffect(() => {
    if (focusReq) setFocusTick(focusReq.nonce)
  }, [focusReq])
  useEffect(() => {
    if (!focusReq || focusTick === 0) return
    ;(focusReq.target.kind === 'graph' ? graphHead.current : selectedHead.current)?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusTick])

  const close = () => selection.select(null)
  const hasInputs = Object.keys(def?.inputs ?? {}).length > 0

  return (
    <aside className="er-insp" aria-label={t('inspector.title')} data-testid="side-panel" data-view={view}>
      <div key={view === 'node' && sel?.kind === 'node' ? `node:${sel.id}` : view} className="er-insp-body">
        {view === 'node' && sel?.kind === 'node' && def && <NodeHeader def={def} doc={doc} id={sel.id} headRef={selectedHead} onClose={close} />}
        {view === 'edge' && sel && sel.kind !== 'node' && <EdgeHeader sel={sel} headRef={selectedHead} onClose={close} />}
        {view === 'graph' && <GraphHeader def={def} headRef={graphHead} />}

        {view === 'node' && sel?.kind === 'node' && (
          <>
            <Inspector g={g} doc={doc} nodeId={sel.id} readOnly={readOnly} onRenamed={(id) => selection.select({ kind: 'node', id })} />
            {def && <Connections def={def} sel={sel} readOnly={readOnly} onRemove={(b) => void g.applyOps([removeOp(b)])} />}
          </>
        )}
        {view === 'edge' && def && sel && sel.kind !== 'node' && <EdgeInspector def={def} edge={sel} picker={picker} />}
        {view === 'graph' && def && hasInputs && (
          <section className="er-sec">
            <div className="er-f">
              <label htmlFor="run-inputs">{t('editor.runInputs')}</label>
              <textarea id="run-inputs" className="er-in mono" value={runInputs} onChange={(e) => onRunInputs(e.target.value)} placeholder="name=value" data-testid="run-inputs" />
            </div>
          </section>
        )}
        {view === 'graph' && (!def || Object.keys(def.nodes).length === 0) && (
          <div className="er-none">
            <b>{t('editor.v2.noStepTitle')}</b>
            <span>{t('editor.v2.noStepText')}</span>
          </div>
        )}
      </div>
      {view !== 'node' && def && Object.keys(def.nodes).length > 0 && <BindingsPanel key="bindings" g={g} def={def} diagnostics={doc.analysis.diagnostics} selection={selection} readOnly={readOnly} />}
      {view === 'graph' && def && <GraphInputsCard g={g} def={def} readOnly={readOnly} />}
      {view === 'graph' && def && <GraphOutputsCard g={g} def={def} readOnly={readOnly} />}
    </aside>
  )
}

function CloseButton({ onClose }: { onClose: () => void }) {
  const { t } = useI18n()
  return (
    <button type="button" className="btn ghost ib er-x" aria-label={t('editor.v2.closeInspector')} title={`${t('editor.v2.closeInspector')} (Esc)`} onClick={onClose} data-testid="inspector-close">
      <Glyph name="close" />
    </button>
  )
}

function NodeHeader({ def, doc, id, headRef, onClose }: { def: Definition; doc: DocState; id: string; headRef: React.RefObject<HTMLHeadingElement | null>; onClose: () => void }) {
  const { t } = useI18n()
  const type = def.nodes[id]!.type
  const known = (NODE_TYPES as readonly string[]).includes(type)
  const b = badgesOf(doc.analysis.diagnostics, def).nodes[id]
  return (
    <div className="er-ih">
      <div className="er-tl">
        <span className={`er-kind k-${type}`}>
          <KindGlyph type={type} />
        </span>
        <h2 tabIndex={-1} ref={headRef} className="er-tl-h" data-testid="selected-heading">
          {known ? t('editor.v2.stepOfType', { type: t(`editor.type.${type as (typeof NODE_TYPES)[number]}`) }) : t('editor.v2.stepOfType', { type })}
          <span className="sr-only"> {id}</span>
        </h2>
        {b && b.errors > 0 ? <Pill tone="x" small>{t('editor.nodeErrors', { count: b.errors })}</Pill> : b && b.warnings > 0 ? <Pill tone="w" small>{t('editor.nodeWarnings', { count: b.warnings })}</Pill> : null}
        <CloseButton onClose={onClose} />
      </div>
    </div>
  )
}

function EdgeHeader({ sel, headRef, onClose }: { sel: Exclude<NonNullable<Selection>, { kind: 'node' }>; headRef: React.RefObject<HTMLHeadingElement | null>; onClose: () => void }) {
  const { t } = useI18n()
  return (
    <div className="er-ih">
      <div className="er-tl">
        <Glyph name="connect" size={14} />
        <span>{sel.kind === 'edge' ? t('editor.connection') : t('editor.v2.graphInputBinding')}</span>
        <CloseButton onClose={onClose} />
      </div>
      <h2 tabIndex={-1} ref={headRef} className="er-ih-title mono" data-testid="selected-heading">
        {sel.from} → {sel.to}
      </h2>
    </div>
  )
}

function GraphHeader({ def, headRef }: { def?: Definition; headRef: React.RefObject<HTMLHeadingElement | null> }) {
  const { t } = useI18n()
  return (
    <div className="er-ih">
      <div className="er-tl">
        <Glyph name="file" size={14} />
        <span>{t('editor.v2.graph')}</span>
      </div>
      {def && (
        <h2 tabIndex={-1} ref={headRef} className="er-ih-title" data-testid="graph-heading">
          {def.name ?? def.id}
        </h2>
      )}
      {def && def.name && <span className="mono mu er-ih-sub">{def.id}</span>}
    </div>
  )
}

/** The node's connections, each removable (an edge, or a graph input binding into it). */
function Connections({ def, sel, readOnly, onRemove }: { def: Definition; sel: { kind: 'node'; id: string }; readOnly: boolean; onRemove: (b: Binding) => void }) {
  const { t } = useI18n()
  const edge = (e: EdgeDef): Binding => ({ kind: 'edge', from: e.from, to: e.to })
  const touches = (r: string) => parseRef(r)?.node === sel.id
  const conns: Binding[] = [
    ...def.edges.filter((e) => touches(e.from) || touches(e.to)).map(edge),
    ...(def.graph_input_bindings ?? []).filter((b) => touches(b.to)).map((b): Binding => ({ kind: 'binding', from: b.from, to: b.to })),
  ]
  return (
    <section className="er-sec" data-testid="selected">
      <h3>{t('editor.connections')}</h3>
      {conns.length === 0 && <p className="er-help">{t('editor.connections.none')}</p>}
      {conns.map((e) => (
        <div className="er-io" key={`${e.kind}:${e.from}->${e.to}`} data-testid="connection">
          <span className="mono er-io-text">
            {e.from} → {e.to}
          </span>
          <span className="mu">{sourceType(def, e.from)}</span>
          <Button size="sm" className="ghost danger r" disabled={readOnly} onClick={() => onRemove(e)} aria-label={t('editor.connection.removeLabel', { from: e.from, to: e.to })} data-testid="remove-edge">
            {t('editor.connection.remove')}
          </Button>
        </div>
      ))}
    </section>
  )
}
