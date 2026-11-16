import { useState } from 'react'
import { useI18n } from '../../i18n/context'
import type { Diagnostic } from '../../types'
import { Button } from '../../ui/primitives'
import type { EdgeDef, NodeDef, NodeInfo, NodeTypeDescriptor, QueryResult } from '../types'
import type { GraphDocument } from '../useGraphDocument'
import { AdvancedSection } from './AdvancedSection'
import { DiagList } from './DiagList'
import { FieldControl } from './FieldControl'
import { activeFields, bindingEnds, diagnosticsExactly, loopOp, loopStarter, PORT_TYPES, withKey } from './model'
import type { Edits } from './useEdits'

// The loop block of a Shell or Agent node (F2.2 requirement 11). set_loop
// replaces the whole block, so every change sends the block as it is with one
// part changed. Which bindings are allowed is asked of the engine by previewing
// each candidate, as can_connect does for edges.

interface Props {
  g: GraphDocument
  nodeId: string
  node: NodeDef
  desc: NodeTypeDescriptor
  info?: NodeInfo
  diags: Diagnostic[]
  readOnly: boolean
  edits: Edits
}

export function LoopSection(p: Props) {
  const { t } = useI18n()
  const [picking, setPicking] = useState<string[] | null>(null)
  const loop = p.node.loop
  const loopDesc = p.desc.loop!
  const where = 'loop'
  if (!loop) {
    const start = (candidate?: string) => {
      const s = loopStarter(p.nodeId, p.node, p.desc, candidate)
      if (s.kind === 'op') void p.edits.apply([s.op], where).then((ok) => ok && setPicking(null))
      else if (s.kind === 'pick') setPicking(s.outputs)
      else setPicking([])
    }
    return (
      <div data-testid="loop-off">
        <p className="mu">{t('inspector.loop.off')}</p>
        {!p.readOnly && (
          <Button size="sm" onClick={() => start()} data-testid="loop-enable">
            {t('inspector.loop.enable')}
          </Button>
        )}
        {picking && picking.length === 0 && <p className="mu" data-testid="loop-no-outputs">{t('inspector.loop.noOutputs')}</p>}
        {picking && picking.length > 0 && (
          <label className="field">
            <span className="lb">{t('inspector.loop.pickCandidate')}</span>
            <select className="input" value="" onChange={(e) => e.target.value && start(e.target.value)} data-testid="loop-candidate">
              <option value="">{t('inspector.field.notSet')}</option>
              {picking.map((o) => (
                <option key={o} value={o}>
                  {o}
                </option>
              ))}
            </select>
          </label>
        )}
        <DiagList diags={[]} refusal={p.edits.refusal(where)} />
      </div>
    )
  }
  const config = loop.gate.config ?? {}
  const inputs = loop.gate.inputs ?? {}
  const shapes = new Set((p.info?.loop_advanced ?? []).filter((a) => a.reason === 'shape').map((a) => a.key))
  const maxField = loopDesc.fields.find((f) => f.key === 'max_attempts')
  return (
    <div className="ins-loop" data-testid="loop-on">
      <ol className="ins-strip" aria-label={t('inspector.loop.strip')}>
        <li>{t('inspector.loop.execute')}</li>
        <li aria-hidden="true">→</li>
        <li>{t('inspector.loop.check')}</li>
        <li aria-hidden="true">→</li>
        <li>{t('inspector.loop.correct')}</li>
      </ol>
      <DiagList diags={diagnosticsExactly(p.diags, `nodes.${p.nodeId}.loop`)} refusal={p.edits.refusal(where)} />
      {maxField && (
        <FieldControl field={maxField} value={loop.max_attempts} present={loop.max_attempts !== undefined && loop.max_attempts !== 0} inputs={{}} diags={[]} refusal={p.edits.refusal('loop.max_attempts')} readOnly={p.readOnly} onCommit={(v) => void p.edits.apply([loopOp(p.nodeId, p.node, { maxAttempts: typeof v === 'number' ? v : null })], 'loop.max_attempts')} />
      )}
      <h4 className="ins-sub">{t('inspector.loop.gateInputs')}</h4>
      <GateInputs {...p} />
      <h4 className="ins-sub">{t('inspector.loop.gate')}</h4>
      <DiagList diags={diagnosticsExactly(p.diags, `nodes.${p.nodeId}.loop.gate`)} />
      {activeFields(loopDesc.gate, config).map((f) => (
        <FieldControl key={f.id} field={f} value={config[f.key!]} present={f.key! in config} inputs={inputs} diags={[]} refusal={p.edits.refusal(`loop.config.${f.key}`)} shape={shapes.has(f.key!)} readOnly={p.readOnly} onCommit={(v) => void p.edits.apply([loopOp(p.nodeId, p.node, { config: withKey(config, f.key!, v) })], `loop.config.${f.key}`)} />
      ))}
      <h4 className="ins-sub">{t('inspector.loop.bindings')}</h4>
      <Bindings {...p} />
      {(p.info?.loop_advanced ?? []).length > 0 && (
        <AdvancedSection entries={p.info!.loop_advanced!} readOnly={p.readOnly} edits={p.edits} prefix="loop.adv" onSet={(key, value) => [loopOp(p.nodeId, p.node, { config: withKey(config, key, value) })]} />
      )}
      {!p.readOnly && (
        <Button size="sm" variant="danger" onClick={() => void p.edits.apply([{ op: 'clear_loop', node: p.nodeId }], where)} data-testid="loop-remove">
          {t('inspector.loop.remove')}
        </Button>
      )}
    </div>
  )
}

function GateInputs(p: Props) {
  const { t } = useI18n()
  const inputs = p.node.loop!.gate.inputs ?? {}
  const [name, setName] = useState('')
  const [type, setType] = useState('json')
  const setInputs = (next: Record<string, { type: string; required?: boolean }>) => p.edits.apply([loopOp(p.nodeId, p.node, { inputs: next })], 'loop.inputs')
  return (
    <div className="ins-ports">
      {Object.keys(inputs).length === 0 && <p className="mu">{t('inspector.ports.none')}</p>}
      <ul className="ins-port-list">
        {Object.entries(inputs).map(([n, d]) => (
          <li key={n} className="row ins-row">
            <span className="mono">{n}</span>
            <span className="tag">{d.type}</span>
            {d.required && <span className="mu">{t('inspector.ports.required')}</span>}
            <Button size="sm" disabled={p.readOnly} onClick={() => void setInputs(Object.fromEntries(Object.entries(inputs).filter(([k]) => k !== n)))} aria-label={t('inspector.ports.removeLabel', { port: n })}>
              {t('inspector.remove')}
            </Button>
          </li>
        ))}
      </ul>
      {!p.readOnly && (
        <form
          className="row ins-row"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim()) void setInputs({ ...inputs, [name.trim()]: { type, required: true } }).then((ok) => ok && setName(''))
          }}
        >
          <input className="input mono" value={name} onChange={(e) => setName(e.target.value)} aria-label={t('inspector.ports.newName')} placeholder={t('inspector.ports.newName')} />
          <select className="input" value={type} onChange={(e) => setType(e.target.value)} aria-label={t('inspector.ports.newType')}>
            {PORT_TYPES.map((ty) => (
              <option key={ty} value={ty}>
                {ty}
              </option>
            ))}
          </select>
          <Button size="sm" type="submit">
            {t('inspector.ports.addInput')}
          </Button>
        </form>
      )}
      <DiagList diags={[]} refusal={p.edits.refusal('loop.inputs')} />
    </div>
  )
}

function Bindings(p: Props) {
  const { t } = useI18n()
  const bindings = p.node.loop!.bindings
  const ends = bindingEnds(p.node)
  const [from, setFrom] = useState('')
  const [checks, setChecks] = useState<Record<string, QueryResult> | null>(null)
  const set = (next: EdgeDef[]) => p.edits.apply([loopOp(p.nodeId, p.node, { bindings: next })], 'loop.bindings')
  const pickSource = async (source: string) => {
    setFrom(source)
    setChecks(null)
    if (!source) return
    const answers = await Promise.all(ends.targets.map((to) => p.g.previewOps([loopOp(p.nodeId, p.node, { bindings: [...bindings, { from: source, to }] })])))
    setChecks(Object.fromEntries(ends.targets.map((to, i) => [to, answers[i]!.ok ? { ok: true } : { ok: false, message: (answers[i] as { message: string }).message }])))
  }
  return (
    <div className="ins-ports" data-testid="loop-bindings">
      {bindings.length === 0 && <p className="mu">{t('inspector.loop.noBindings')}</p>}
      <ul className="ins-port-list">
        {bindings.map((b, i) => (
          <li key={`${b.from}->${b.to}`} className="row ins-row">
            <span className="mono">
              {b.from} → {b.to}
            </span>
            <Button size="sm" disabled={p.readOnly} onClick={() => void set(bindings.filter((_, j) => j !== i))} aria-label={t('inspector.loop.removeBinding', { from: b.from, to: b.to })}>
              {t('inspector.remove')}
            </Button>
          </li>
        ))}
      </ul>
      {!p.readOnly && (
        <div className="row ins-row">
          <select className="input mono" value={from} onChange={(e) => void pickSource(e.target.value)} aria-label={t('inspector.loop.bindingFrom')} data-testid="binding-from">
            <option value="">{t('inspector.loop.bindingFrom')}</option>
            {ends.sources.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          {from && !checks && <span className="mu">{t('editor.connect.checking')}</span>}
          {from && checks && (
            <select className="input mono" value="" aria-label={t('inspector.loop.bindingTo')} onChange={(e) => e.target.value && void set([...bindings, { from, to: e.target.value }]).then((ok) => ok && void pickSource(''))} data-testid="binding-to">
              <option value="">{Object.values(checks).some((c) => c.ok) ? t('inspector.loop.bindingTo') : t('inspector.loop.noTargets')}</option>
              {Object.entries(checks)
                .filter(([, c]) => c.ok)
                .map(([to]) => (
                  <option key={to} value={to}>
                    {to}
                  </option>
                ))}
            </select>
          )}
        </div>
      )}
      <DiagList diags={[]} refusal={p.edits.refusal('loop.bindings')} />
    </div>
  )
}
