import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import type { Diagnostic } from '../types'
import { Button, Card, Pill, StateMessage } from '../ui/primitives'
import { AddBindingForm, type Changed } from './AddBindingForm'
import { bindingRows, removeOp, type BindingRow } from './bindings'
import { hintsOf } from './hints'
import { NeedsConnection } from './NeedsConnection'
import type { SelectionOwner } from './selection'
import type { Definition } from './types'
import type { GraphDocument } from './useGraphDocument'

// The Bindings panel (F2.3 requirement 3): every edge and graph input binding in
// one table, in a stable order, with the declared type, the engine's status for
// it and Remove. It shares the editor's selection with the canvas. It lists what
// the engine returned and sends operations; whether a binding is valid is the
// engine's diagnostics.

type Focus = { from: string; to: string } | 'heading' | 'add'

const isSelected = (row: BindingRow, s: SelectionOwner['selection']) => s !== null && s.kind !== 'node' && s.kind === row.kind && s.from === row.from && s.to === row.to

interface Props {
  g: GraphDocument
  def: Definition
  diagnostics: Diagnostic[]
  selection: SelectionOwner
  /** The canvas's read-only reason: the file does not parse or uses anchors. */
  readOnly: boolean
}

export function BindingsPanel({ g, def, diagnostics, selection, readOnly }: Props) {
  const { t } = useI18n()
  const rows = bindingRows(def, diagnostics)
  const hints = hintsOf(diagnostics, def)
  const [adding, setAdding] = useState(false)
  const [announce, setAnnounce] = useState('')
  const [failure, setFailure] = useState<string | null>(null)
  const [focusAfter, setFocusAfter] = useState<Focus | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const heading = useRef<HTMLSpanElement>(null)
  const addWrap = useRef<HTMLSpanElement>(null)

  // Focus moves once the rows it names are on screen.
  useEffect(() => {
    if (!focusAfter) return
    const el =
      focusAfter === 'heading'
        ? heading.current
        : focusAfter === 'add'
          ? addWrap.current?.querySelector('button')
          : [...(root.current?.querySelectorAll<HTMLElement>('[data-testid="binding-select"]') ?? [])].find((b) => b.dataset.from === focusAfter.from && b.dataset.to === focusAfter.to)
    if (!el) return
    el.focus()
    setFocusAfter(null)
  }, [focusAfter, rows.length])

  const remove = async (row: BindingRow) => {
    setFailure(null)
    const at = rows.indexOf(row)
    const next = rows[at + 1] ?? rows[at - 1]
    const r = await g.applyOps([removeOp(row)], { quiet: true })
    if (!r.ok) return setFailure(r.message)
    setAnnounce(t('editor.bindings.announce.removed', { from: row.from, to: row.to }))
    setFocusAfter(next ? { from: next.from, to: next.to } : 'heading')
  }

  const changed = (c: Changed) => {
    setAdding(false)
    setFailure(null)
    setAnnounce(c.announce)
    setFocusAfter(c.focus)
  }

  return (
    <Card
      title={
        <span tabIndex={-1} ref={heading} className="ed-panel-head" data-testid="bindings-heading">
          {t('editor.bindings.title')}
        </span>
      }
      testId="bindings"
      action={
        <span ref={addWrap}>
          <Button size="sm" disabled={readOnly || adding} onClick={() => setAdding(true)} aria-expanded={adding} data-testid="add-binding-open">
            {t('editor.bindings.add')}
          </Button>
        </span>
      }
    >
      <div className="ed-bindings" ref={root}>
        <div className="sr-only" role="status" aria-live="polite" data-testid="bindings-live">
          {announce}
        </div>
        {readOnly && (
          <p className="mu" data-testid="bindings-readonly">
            {t('editor.bindings.readonly')}
          </p>
        )}
        <NeedsConnection g={g} def={def} hints={hints} readOnly={readOnly} announce={setAnnounce} />
        {adding && !readOnly && <AddBindingForm g={g} def={def} onChanged={changed} onCancel={() => { setAdding(false); setFocusAfter('add') }} />}
        {failure && (
          <p className="ed-refusal" role="alert" data-testid="bindings-failure">
            {t('editor.opFailed', { message: failure })}
          </p>
        )}
        {rows.length === 0 ? (
          <StateMessage kind="empty" testId="bindings-empty">
            {t('editor.bindings.empty')}
          </StateMessage>
        ) : (
          <table className="ed-bindings-table" data-testid="bindings-table">
            <caption className="sr-only">{t('editor.bindings.caption')}</caption>
            <thead>
              <tr>
                <th scope="col">{t('editor.bindings.col.binding')}</th>
                <th scope="col">{t('editor.bindings.col.type')}</th>
                <th scope="col">{t('editor.bindings.col.status')}</th>
                <th scope="col">
                  <span className="sr-only">{t('editor.bindings.col.actions')}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <Row key={`${row.kind}:${row.index}`} row={row} selected={isSelected(row, selection.selection)} readOnly={readOnly} onSelect={() => selection.select({ kind: row.kind, from: row.from, to: row.to })} onRemove={() => void remove(row)} />
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}

function Row({ row, selected, readOnly, onSelect, onRemove }: { row: BindingRow; selected: boolean; readOnly: boolean; onSelect: () => void; onRemove: () => void }) {
  const { t } = useI18n()
  return (
    <tr className={selected ? 'sel' : undefined} data-testid="binding-row" data-kind={row.kind} data-from={row.from} data-to={row.to}>
      <th scope="row">
        <button type="button" className="ed-binding-select" aria-pressed={selected} onClick={onSelect} data-testid="binding-select" data-from={row.from} data-to={row.to}>
          <span className="mono">{row.from}</span> <span aria-hidden="true">→</span>
          <span className="sr-only"> {t('editor.bindings.to')} </span> <span className="mono">{row.to}</span>
        </button>
      </th>
      <td className="mono">{row.type || '—'}</td>
      <td>
        <Status row={row} />
      </td>
      <td>
        <Button size="sm" variant="danger" disabled={readOnly} onClick={onRemove} aria-label={t('editor.connection.removeLabel', { from: row.from, to: row.to })} data-testid="remove-binding">
          {t('editor.connection.remove')}
        </Button>
      </td>
    </tr>
  )
}

// Words and glyphs, never colour alone.
function Status({ row }: { row: BindingRow }) {
  const { t } = useI18n()
  if (row.errors > 0) return <Pill tone="x" small>{'✖ '}{t('editor.nodeErrors', { count: row.errors })}</Pill>
  if (row.warnings > 0) return <Pill tone="w" small>{'⚠ '}{t('editor.nodeWarnings', { count: row.warnings })}</Pill>
  return <span className="mu">{'✓ '}{t('editor.bindings.statusOk')}</span>
}
