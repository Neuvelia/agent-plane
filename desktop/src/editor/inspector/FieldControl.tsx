import { useId, useState, type ReactNode } from 'react'
import { useI18n } from '../../i18n/context'
import type { Diagnostic } from '../../types'
import { Button } from '../../ui/primitives'
import type { FieldDescriptor, JsonValue, PortDef, Rule } from '../types'
import { fieldText } from './fieldText'
import { keyTaken, parseInteger, parseLoose } from './model'
import { TextCommit } from './TextCommit'

// One descriptor, one native control (F2.2 requirement 7). The control shows the
// file's value and commits what the person typed, unchanged: whether it is valid
// is the engine's diagnostic, shown beside the field and tied to it with
// aria-describedby. No rule is evaluated here.

export interface FieldProps {
  field: FieldDescriptor
  value: unknown
  present: boolean
  /** Declared inputs of the node (or of the loop's Gate), for input pickers. */
  inputs: Record<string, PortDef>
  diags: Diagnostic[]
  /** The engine refused the last change of this field. */
  refusal?: string
  /** The value does not fit this control; it is shown and edited under Advanced. */
  shape?: boolean
  readOnly: boolean
  /** May return the edit's promise: false (refused) puts the file's value back into a text control. */
  onCommit: (value: JsonValue | undefined) => unknown
}

const asText = (v: unknown): string => (v === undefined || v === null ? '' : typeof v === 'string' ? v : JSON.stringify(v))
const strings = (v: unknown): string[] => (Array.isArray(v) ? v.map(asText) : [])
const entries = (v: unknown): [string, string][] => (v && typeof v === 'object' && !Array.isArray(v) ? Object.entries(v as Record<string, unknown>).map(([k, x]) => [k, asText(x)]) : [])

/** Inputs a picker offers: those of the descriptor's input types, plus the current value so it is never hidden. */
function inputChoices(field: FieldDescriptor, inputs: Record<string, PortDef>, current?: string): string[] {
  const names = Object.entries(inputs)
    .filter(([, p]) => !field.input_types || field.input_types.includes(p.type))
    .map(([n]) => n)
  return current && !names.includes(current) ? [...names, current] : names
}

export function FieldControl(p: FieldProps) {
  const { t, locale } = useI18n()
  const uid = useId()
  const f = p.field
  const text = fieldText(locale, f.id, f.key)
  const id = `${uid}-control`
  const helpId = `${uid}-help`
  const diagId = `${uid}-diag`
  // Text the control cannot turn into a value (an integer field holding `abc`, a map key typed twice) is refused here, never sent as something else.
  const [local, setLocal] = useState<string | undefined>()
  const describedBy = [text.help || f.requires || f.excludes ? helpId : '', p.diags.length || p.refusal || local ? diagId : ''].filter(Boolean).join(' ')
  const invalid = p.diags.some((d) => d.severity === 'error') || !!p.refusal || !!local
  const common = { id, describedBy, invalid, required: f.required, disabled: p.readOnly }
  const canClear = p.present && !f.required && f.kind !== 'bool' && !f.locked && !p.readOnly

  let control: ReactNode
  if (p.shape) control = <p className="mu" data-testid="field-shape">{t('inspector.field.shape')}</p>
  else if (f.locked) control = <Locked {...p} id={id} describedBy={describedBy} />
  else control = <Control {...p} common={common} label={text.label} setLocal={setLocal} />

  return (
    <div className="ins-field" data-testid={`field-${f.id}`} data-field-key={f.key}>
      <div className="row between">
        <label className="lb" htmlFor={f.kind === 'list' || f.kind === 'path_list' || f.kind === 'map' || p.shape ? undefined : id} id={`${uid}-label`}>
          {text.label}
          {f.required && (
            <span className="ins-req" aria-hidden="true">
              {' *'}
            </span>
          )}
          {f.required && <span className="sr-only"> {t('inspector.field.required')}</span>}
          <span className="ins-key mono"> {f.key}</span>
        </label>
        {canClear && (
          <Button size="sm" onClick={() => p.onCommit(undefined)} aria-label={t('inspector.field.clearLabel', { label: text.label })} data-testid={`clear-${f.key}`}>
            {t('inspector.field.clear')}
          </Button>
        )}
      </div>
      {control}
      {(text.help || f.requires || f.excludes) && (
        <p className="ins-help mu" id={helpId}>
          {text.help}
          {f.requires && <> {t('inspector.rule.requires', { rules: rulesText(f.requires, t) })}</>}
          {f.excludes && <> {t(f.required ? 'inspector.rule.exactlyOne' : 'inspector.rule.excludes', { rules: rulesText(f.excludes, t) })}</>}
        </p>
      )}
      {(p.diags.length > 0 || p.refusal || local) && (
        <ul className="ins-diags" id={diagId} data-testid="field-diagnostics">
          {local && <li className="err" data-testid="field-local-error">{local}</li>}
          {p.refusal && <li className="err">{t('inspector.refused', { message: p.refusal })}</li>}
          {p.diags.map((d, i) => (
            <li key={i} className={d.severity === 'error' ? 'err' : 'warn'}>
              <span aria-hidden="true">{d.severity === 'error' ? '✖ ' : '⚠ '}</span>
              {d.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function rulesText(rules: Rule[], t: ReturnType<typeof useI18n>['t']): string {
  return rules.map((r) => (r.input ? t('inspector.rule.input', { input: r.input }) : r.equals ? `${r.key} = ${r.equals}` : (r.key ?? ''))).join(', ')
}

function Locked(p: FieldProps & { id: string; describedBy: string }) {
  const { t } = useI18n()
  const fixed = p.field.locked!
  const differs = !p.present || asText(p.value) !== asText(fixed.value)
  return (
    <div className="row">
      <input id={p.id} className="input mono" readOnly value={asText(p.present ? p.value : fixed.value)} aria-describedby={p.describedBy || undefined} />
      <span className="mu">{t('inspector.field.locked', { reason: fixed.reason })}</span>
      {differs && !p.readOnly && (
        <Button size="sm" onClick={() => p.onCommit(fixed.value as JsonValue)}>
          {t('inspector.field.useLocked', { value: asText(fixed.value) })}
        </Button>
      )}
    </div>
  )
}

type Common = { id: string; describedBy: string; invalid: boolean; required?: boolean; disabled: boolean }

type SetLocal = (message: string | undefined) => void

function Control(p: FieldProps & { common: Common; label: string; setLocal: SetLocal }) {
  const { t } = useI18n()
  const f = p.field
  const c = p.common
  const aria = { id: c.id, 'aria-describedby': c.describedBy || undefined, 'aria-invalid': c.invalid || undefined, 'aria-required': c.required || undefined, disabled: c.disabled }
  switch (f.kind) {
    case 'bool':
      return <input type="checkbox" {...aria} className="ins-check" checked={p.value === true} onChange={(e) => p.onCommit(e.target.checked)} />
    case 'enum':
    case 'input_name': {
      const current = typeof p.value === 'string' ? p.value : undefined
      const options = f.kind === 'enum' ? [...(f.enum ?? []), ...(current && !f.enum?.includes(current) ? [current] : [])] : inputChoices(f, p.inputs, current)
      return (
        <select {...aria} className="input" value={current ?? ''} onChange={(e) => p.onCommit(e.target.value === '' ? undefined : e.target.value)}>
          <option value="">{f.kind === 'input_name' && options.length === 0 ? t('inspector.field.noInputs') : t('inspector.field.notSet')}</option>
          {options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      )
    }
    case 'int':
      return (
        <TextCommit
          {...c}
          numeric
          value={asText(p.value)}
          onCommit={(s) => {
            const n = parseInteger(s)
            if (n === null) return p.setLocal(t('inspector.field.notInteger', { text: s.trim() }))
            p.setLocal(undefined)
            return p.onCommit(n === '' ? undefined : n)
          }}
        />
      )
    case 'scalar':
      return <TextCommit {...c} className="mono" value={asText(p.value)} onCommit={(s) => p.onCommit(/^(true|false|null|-?\d+)$/.test(s.trim()) ? parseLoose(s.trim()) : s)} />
    case 'text':
      return <TextCommit {...c} multiline value={asText(p.value)} onCommit={(s) => p.onCommit(s)} />
    case 'list':
    case 'path_list':
      return <ListEditor {...p} label={p.label} />
    case 'map':
      return <MapEditor {...p} label={p.label} setLocal={p.setLocal} />
    case 'group':
      // No schema-v0 descriptor is a keyed group; should one appear, its value is edited under Advanced.
      return <p className="mu">{t('inspector.field.shape')}</p>
    default:
      return <TextCommit {...c} className={f.kind === 'string' ? 'mono' : ''} value={asText(p.value)} onCommit={(s) => p.onCommit(s)} />
  }
}

function ListEditor(p: FieldProps & { label: string }) {
  const { t, locale } = useI18n()
  const items = strings(p.value)
  const item = p.field.item
  const itemLabel = item ? fieldText(locale, item.id, item.key).label : p.label
  const full = p.field.max_items !== undefined && items.length >= p.field.max_items
  const set = (next: string[]) => p.onCommit(next)
  const picker = item?.kind === 'input_name'
  const remaining = picker ? inputChoices(item, p.inputs).filter((n) => !items.includes(n)) : []
  return (
    <div className="ins-list" role="group" aria-label={p.label} data-testid={`list-${p.field.key}`}>
      {items.length === 0 && <p className="mu">{t('inspector.list.empty')}</p>}
      {items.map((v, i) => (
        <div className="row ins-row" key={i}>
          <TextCommit value={v} label={t('inspector.list.item', { label: itemLabel, n: i + 1 })} disabled={p.readOnly || picker} className="mono" onCommit={(s) => set(items.map((x, j) => (j === i ? s : x)))} testId={`list-${p.field.key}-${i}`} />
          <Button size="sm" disabled={p.readOnly} onClick={() => set(items.filter((_, j) => j !== i))} aria-label={t('inspector.list.removeLabel', { label: itemLabel, n: i + 1 })}>
            {t('inspector.remove')}
          </Button>
        </div>
      ))}
      {!p.readOnly && !full && picker && remaining.length > 0 && (
        <select className="input" aria-label={t('inspector.list.addLabel', { label: itemLabel })} value="" onChange={(e) => e.target.value && set([...items, e.target.value])} data-testid={`list-${p.field.key}-add`}>
          <option value="">{t('inspector.list.addPick', { label: itemLabel })}</option>
          {remaining.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      )}
      {!p.readOnly && !full && !picker && <TextCommit value="" keepDraft placeholder={t('inspector.list.addPlaceholder', { label: itemLabel })} label={t('inspector.list.addLabel', { label: itemLabel })} className="mono" onCommit={(s) => s !== '' && set([...items, s])} testId={`list-${p.field.key}-add`} />}
      {full && <p className="mu">{t('inspector.list.full', { max: p.field.max_items ?? 0 })}</p>}
    </div>
  )
}

function MapEditor(p: FieldProps & { label: string; setLocal: SetLocal }) {
  const { t } = useI18n()
  const rows = entries(p.value)
  const obj = (list: [string, string][]): JsonValue => Object.fromEntries(list)
  // A key another row already has would silently replace that entry; it is refused with a message instead.
  const taken = (key: string, except: number) => keyTaken(rows, key, except)
  const refuseTaken = (key: string) => {
    p.setLocal(t('inspector.map.keyTaken', { key }))
    return false
  }
  const keyPicker = !!p.field.input_types
  const freeKeys = keyPicker ? inputChoices(p.field, p.inputs).filter((n) => !rows.some(([k]) => k === n)) : []
  return (
    <div className="ins-list" role="group" aria-label={p.label} data-testid={`map-${p.field.key}`}>
      {rows.length === 0 && <p className="mu">{t('inspector.list.empty')}</p>}
      {rows.map(([k, v], i) => (
        <div className="row ins-row" key={i}>
          <TextCommit value={k} className="mono" label={t('inspector.map.key', { label: p.label, n: i + 1 })} disabled={p.readOnly || keyPicker} onCommit={(s) => {
              if (taken(s, i)) return refuseTaken(s)
              p.setLocal(undefined)
              return p.onCommit(obj(rows.map((r, j) => (j === i ? [s, r[1]] : r))))
            }}
          />
          <span aria-hidden="true">=</span>
          <TextCommit value={v} className="mono" label={t('inspector.map.value', { label: p.label, key: k })} disabled={p.readOnly} onCommit={(s) => p.onCommit(obj(rows.map((r, j) => (j === i ? [r[0], s] : r))))} />
          <Button size="sm" disabled={p.readOnly} onClick={() => p.onCommit(obj(rows.filter((_, j) => j !== i)))} aria-label={t('inspector.map.removeLabel', { label: p.label, key: k })}>
            {t('inspector.remove')}
          </Button>
        </div>
      ))}
      {!p.readOnly && (keyPicker ? freeKeys.length > 0 : true) && (
        <form
          className="row ins-row"
          onSubmit={(e) => {
            e.preventDefault()
            const data = new FormData(e.currentTarget)
            const key = String(data.get('key') ?? '').trim()
            if (!key) return
            if (taken(key, -1)) return void refuseTaken(key)
            p.setLocal(undefined)
            p.onCommit(obj([...rows, [key, String(data.get('value') ?? '')]]))
            e.currentTarget.reset()
          }}
        >
          {keyPicker ? (
            <select name="key" className="input" aria-label={t('inspector.map.newKey', { label: p.label })} defaultValue="">
              <option value="">{t('inspector.field.notSet')}</option>
              {freeKeys.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          ) : (
            <input name="key" className="input mono" aria-label={t('inspector.map.newKey', { label: p.label })} placeholder={t('inspector.map.keyPlaceholder')} />
          )}
          <input name="value" className="input mono" aria-label={t('inspector.map.newValue', { label: p.label })} placeholder={t('inspector.map.valuePlaceholder')} />
          <Button size="sm" type="submit">
            {t('inspector.add')}
          </Button>
        </form>
      )}
    </div>
  )
}
