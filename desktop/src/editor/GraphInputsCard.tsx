import { useId, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Card, StateMessage } from '../ui/primitives'
import { AddBindingForm } from './AddBindingForm'
import { removeOp } from './bindings'
import { graphInputRows } from './graphData'
import { describeChange, PORT_TYPES, type Change } from './inspector/model'
import { TextCommit } from './inspector/TextCommit'
import type { Definition, Op } from './types'
import type { GraphDocument } from './useGraphDocument'

// Graph inputs (F2.3 requirement 5): declare, rename, retype, mark required and
// remove, and bind each to node inputs with the engine's compatibility answers.
// A graph input is a declaration, never a value.

interface Props {
  g: GraphDocument
  def: Definition
  readOnly: boolean
}

interface Removal {
  name: string
  changes: Change[]
}

export function GraphInputsCard({ g, def, readOnly }: Props) {
  const { t } = useI18n()
  const [said, announce] = useState('')
  const uid = useId()
  const rows = graphInputRows(def)
  const [name, setName] = useState('')
  const [type, setType] = useState<string>('string')
  const [required, setRequired] = useState(true)
  const [refusal, setRefusal] = useState<string | null>(null)
  const [binding, setBinding] = useState<string | null>(null)
  const [removal, setRemoval] = useState<Removal | null>(null)
  const errorId = `${uid}-error`

  // Every change is one batch; a refusal is shown here and changes nothing.
  const send = async (ops: Op[], done?: string): Promise<boolean> => {
    const r = await g.applyOps(ops, { quiet: true })
    setRefusal(r.ok ? null : r.message)
    if (r.ok && done) announce(done)
    return r.ok
  }

  // Removing an input that has bindings is previewed: the engine says what goes with it.
  const askRemove = async (input: string) => {
    const ops: Op[] = [{ op: 'remove_graph_input', id: input }]
    const preview = await g.previewOps(ops)
    if (!preview.ok) return setRefusal(preview.message)
    const changes = preview.doc.definition ? describeChange(def, preview.doc.definition).filter((c) => c.what !== 'port') : []
    if (changes.some((c) => c.what === 'binding')) return setRemoval({ name: input, changes })
    await send(ops, t('editor.graphInputs.announce.removed', { name: input }))
  }

  return (
    <Card title={t('editor.graphInputs.title')} testId="graph-inputs">
      <div className="ed-bindings" role="region" aria-label={t('editor.graphInputs.title')}>
        <div className="sr-only" role="status" aria-live="polite">
          {said}
        </div>
        {rows.length === 0 && (
          <StateMessage kind="empty" testId="graph-inputs-empty">
            {t('editor.graphInputs.empty')}
          </StateMessage>
        )}
        <ul className="ins-port-list">
          {rows.map((r) => (
            <li key={r.name} className="ins-port" data-testid="graph-input" data-name={r.name}>
              <div className="row">
                <TextCommit value={r.name} className="mono" label={t('editor.graphInputs.nameLabel', { name: r.name })} describedBy={refusal ? errorId : undefined} disabled={readOnly} onCommit={(s) => send([{ op: 'update_graph_input', id: r.name, new_id: s.trim() }])} testId={`graph-input-name-${r.name}`} />
                <select className="input" aria-label={t('editor.graphInputs.typeLabel', { name: r.name })} aria-describedby={refusal ? errorId : undefined} disabled={readOnly} value={r.type} onChange={(e) => void send([{ op: 'update_graph_input', id: r.name, type: e.target.value }])} data-testid={`graph-input-type-${r.name}`}>
                  {[...new Set([r.type, ...PORT_TYPES])].map((ty) => (
                    <option key={ty} value={ty}>
                      {ty}
                    </option>
                  ))}
                </select>
                <label className="row ins-inline">
                  <input type="checkbox" className="ins-check" disabled={readOnly} checked={r.required} aria-label={t('editor.graphInputs.requiredLabel', { name: r.name })} onChange={(e) => void send([{ op: 'update_graph_input', id: r.name, required: e.target.checked }])} data-testid={`graph-input-required-${r.name}`} />
                  <span aria-hidden="true">{t('editor.graphInputs.required')}</span>
                </label>
                <Button size="sm" variant="danger" disabled={readOnly} aria-label={t('editor.graphInputs.removeLabel', { name: r.name })} onClick={() => void askRemove(r.name)} data-testid={`remove-graph-input-${r.name}`}>
                  {t('editor.graphInputs.remove')}
                </Button>
              </div>
              <div className="mu">
                {t('editor.graphInputs.boundTo')}: {r.bindings.length === 0 && t('editor.graphInputs.noBindings')}
              </div>
              {r.bindings.map((b) => (
                <div className="row between" key={b.to} data-testid="graph-input-binding">
                  <span className="mono">{b.to}</span>
                  <Button size="sm" variant="danger" disabled={readOnly} aria-label={t('editor.connection.removeLabel', { from: b.from, to: b.to })} onClick={() => void send([removeOp(b)], t('editor.bindings.announce.removed', { from: b.from, to: b.to }))} data-testid="remove-input-binding">
                    {t('editor.connection.remove')}
                  </Button>
                </div>
              ))}
              {binding === r.name ? (
                <AddBindingForm
                  g={g}
                  def={def}
                  fixedSource={`inputs.${r.name}`}
                  onCancel={() => setBinding(null)}
                  onChanged={(c) => {
                    setBinding(null)
                    announce(c.announce)
                  }}
                />
              ) : (
                <div className="row">
                  <Button size="sm" disabled={readOnly} aria-label={t('editor.graphInputs.bindLabel', { name: r.name })} onClick={() => setBinding(r.name)} data-testid={`bind-graph-input-${r.name}`}>
                    {t('editor.graphInputs.bind')}
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
        {removal && (
          <div className="banner warn ins-preview" role="alertdialog" aria-label={t('editor.graphInputs.previewTitle', { name: removal.name })} data-testid="graph-input-preview">
            <div>
              <b>{t('editor.graphInputs.previewTitle', { name: removal.name })}</b>
              <ul>
                {removal.changes.map((c, i) => (
                  <li key={i}>
                    {t(`inspector.preview.${c.how}`, { what: t(`inspector.preview.what.${c.what}`) })} <span className="mono">{c.text}</span>
                  </li>
                ))}
              </ul>
            </div>
            <span className="row">
              <Button size="sm" variant="primary" data-testid="graph-input-preview-apply" onClick={() => void send([{ op: 'remove_graph_input', id: removal.name }], t('editor.graphInputs.announce.removed', { name: removal.name })).then((ok) => ok && setRemoval(null))}>
                {t('editor.graphInputs.previewApply')}
              </Button>
              <Button size="sm" onClick={() => setRemoval(null)} data-testid="graph-input-preview-cancel">
                {t('editor.cancel')}
              </Button>
            </span>
          </div>
        )}
        {!readOnly && (
          <form
            className="row ins-row"
            data-testid="add-graph-input"
            onSubmit={(e) => {
              e.preventDefault()
              if (!name.trim()) return
              void send([{ op: 'add_graph_input', id: name.trim(), type, required }], t('editor.graphInputs.announce.added', { name: name.trim() })).then((ok) => ok && setName(''))
            }}
          >
            <input className="input mono" value={name} onChange={(e) => setName(e.target.value)} aria-label={t('editor.graphInputs.newName')} aria-describedby={refusal ? errorId : undefined} placeholder={t('editor.graphInputs.newName')} data-testid="add-graph-input-name" />
            <select className="input" value={type} onChange={(e) => setType(e.target.value)} aria-label={t('editor.graphInputs.newType')}>
              {PORT_TYPES.map((ty) => (
                <option key={ty} value={ty}>
                  {ty}
                </option>
              ))}
            </select>
            <label className="row ins-inline">
              <input type="checkbox" className="ins-check" checked={required} onChange={(e) => setRequired(e.target.checked)} />
              {t('editor.graphInputs.required')}
            </label>
            <Button size="sm" type="submit" data-testid="add-graph-input-apply">
              {t('editor.graphInputs.add')}
            </Button>
          </form>
        )}
        {refusal && (
          <p className="ed-refusal" id={errorId} role="alert" data-testid="graph-inputs-refusal">
            {t('editor.opFailed', { message: refusal })}
          </p>
        )}
      </div>
    </Card>
  )
}
