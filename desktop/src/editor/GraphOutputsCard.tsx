import { useId, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Card, StateMessage } from '../ui/primitives'
import { outputChoices, outputOptions, outputRows } from './graphData'
import type { Definition, Op } from './types'
import type { GraphDocument } from './useGraphDocument'

// Graph outputs (F2.3 requirement 6): add, change the source and remove. Only
// required node outputs are offered (decision 8); an existing output on an
// optional port is shown as written and cannot be chosen again.

interface Props {
  g: GraphDocument
  def: Definition
  readOnly: boolean
}

export function GraphOutputsCard({ g, def, readOnly }: Props) {
  const { t } = useI18n()
  const [said, announce] = useState('')
  const uid = useId()
  const rows = outputRows(def)
  const choices = outputChoices(def)
  const [name, setName] = useState('')
  const [from, setFrom] = useState('')
  const [refusal, setRefusal] = useState<string | null>(null)
  const errorId = `${uid}-error`

  const send = async (ops: Op[], done: string): Promise<boolean> => {
    const r = await g.applyOps(ops, { quiet: true })
    setRefusal(r.ok ? null : r.message)
    if (r.ok) announce(done)
    return r.ok
  }

  return (
    <Card title={t('editor.graphOutputs.title')} testId="graph-outputs">
      <div className="ed-bindings" role="region" aria-label={t('editor.graphOutputs.title')}>
        <div className="sr-only" role="status" aria-live="polite">
          {said}
        </div>
        {rows.length === 0 && (
          <StateMessage kind="empty" testId="graph-outputs-empty">
            {t('editor.graphOutputs.empty')}
          </StateMessage>
        )}
        <ul className="ins-port-list">
          {rows.map((r) => (
            <li key={r.name} className="ins-port" data-testid="graph-output" data-name={r.name}>
              <div className="row">
                <b className="mono">{r.name}</b>
                <select className="input mono" aria-label={t('editor.graphOutputs.sourceLabel', { name: r.name })} aria-describedby={refusal ? errorId : undefined} disabled={readOnly} value={r.from} onChange={(e) => void send([{ op: 'set_graph_output', id: r.name, from: e.target.value }], t('editor.graphOutputs.announce.set', { name: r.name, from: e.target.value }))} data-testid={`graph-output-source-${r.name}`}>
                  {outputOptions(def, r.from).map((ref) => (
                    <option key={ref} value={ref}>
                      {ref}
                    </option>
                  ))}
                </select>
                <Button size="sm" variant="danger" disabled={readOnly} aria-label={t('editor.graphOutputs.removeLabel', { name: r.name })} onClick={() => void send([{ op: 'remove_graph_output', id: r.name }], t('editor.graphOutputs.announce.removed', { name: r.name }))} data-testid={`remove-graph-output-${r.name}`}>
                  {t('editor.graphOutputs.remove')}
                </Button>
              </div>
              {r.optionalSource && <p className="mu">{t('editor.graphOutputs.optionalKept')}</p>}
            </li>
          ))}
        </ul>
        {!readOnly && choices.length === 0 && <p className="mu">{t('editor.graphOutputs.noRequired')}</p>}
        {!readOnly && choices.length > 0 && (
          <form
            className="row ins-row"
            data-testid="add-graph-output"
            onSubmit={(e) => {
              e.preventDefault()
              if (!name.trim() || !from) return
              void send([{ op: 'set_graph_output', id: name.trim(), from }], t('editor.graphOutputs.announce.set', { name: name.trim(), from })).then((ok) => {
                if (ok) {
                  setName('')
                  setFrom('')
                }
              })
            }}
          >
            <input className="input mono" value={name} onChange={(e) => setName(e.target.value)} aria-label={t('editor.graphOutputs.newName')} aria-describedby={refusal ? errorId : undefined} placeholder={t('editor.graphOutputs.newName')} data-testid="add-graph-output-name" />
            <select className="input mono" value={from} onChange={(e) => setFrom(e.target.value)} aria-label={t('editor.graphOutputs.source')} data-testid="add-graph-output-source">
              <option value="">{t('editor.graphOutputs.sourcePlaceholder')}</option>
              {choices.map((ref) => (
                <option key={ref} value={ref}>
                  {ref}
                </option>
              ))}
            </select>
            <Button size="sm" type="submit" data-testid="add-graph-output-apply">
              {t('editor.graphOutputs.add')}
            </Button>
          </form>
        )}
        {refusal && (
          <p className="ed-refusal" id={errorId} role="alert" data-testid="graph-outputs-refusal">
            {t('editor.opFailed', { message: refusal })}
          </p>
        )}
      </div>
    </Card>
  )
}
