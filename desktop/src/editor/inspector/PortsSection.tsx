import { useState } from 'react'
import { useI18n } from '../../i18n/context'
import type { Diagnostic } from '../../types'
import { Button } from '../../ui/primitives'
import type { Definition, NodeDef, NodeTypeDescriptor, Op, PortDirection } from '../types'
import { diagnosticsAt, PORT_TYPES, type Source } from './model'
import { TextCommit } from './TextCommit'
import type { Edits } from './useEdits'

// Inputs and outputs of the selected node (F2.2 requirements 9 and 10). A
// rename, retype or removal is first previewed by the engine; when it touches
// anything besides the port itself, the person sees the list and confirms.

interface Props {
  def: Definition
  nodeId: string
  node: NodeDef
  desc?: NodeTypeDescriptor
  direction: PortDirection
  sources?: Record<string, Source[]>
  diags: Diagnostic[]
  readOnly: boolean
  edits: Edits
}

export function PortsSection(p: Props) {
  const { t } = useI18n()
  const side = p.direction === 'input' ? 'inputs' : 'outputs'
  const ports = Object.entries((p.direction === 'input' ? p.node.inputs : p.node.outputs) ?? {})
  const locked = p.direction === 'output' && p.desc?.outputs_locked
  const allowed = p.direction === 'output' && p.desc ? p.desc.outputs : undefined
  const free = allowed?.filter((o) => !ports.some(([n]) => n === o.name)) ?? []
  const where = (port: string) => `${side}.${port}`
  const ro = p.readOnly || !!locked
  return (
    <div className="ins-ports" data-testid={`ports-${side}`}>
      {ports.length === 0 && <p className="mu">{t('inspector.ports.none')}</p>}
      {locked && <p className="mu" data-testid="outputs-locked">{t('inspector.ports.locked')}</p>}
      <ul className="ins-port-list">
        {ports.map(([name, port]) => {
          const diags = diagnosticsAt(p.diags, `nodes.${p.nodeId}.${side}.${name}`)
          const op = (extra: Partial<Extract<Op, { op: 'update_port' }>>): Op => ({ op: 'update_port', node: p.nodeId, direction: p.direction, port: name, ...extra })
          const refusal = p.edits.refusal(where(name))
          return (
            <li key={name} className="ins-port" data-testid={`port-${side}-${name}`}>
              <div className="row">
                {allowed ? (
                  <select className="input mono" aria-label={t('inspector.ports.name', { port: name })} disabled={ro} value={name} onChange={(e) => void p.edits.intend([op({ new_port: e.target.value })], where(name))}>
                    {[name, ...free.map((o) => o.name)].map((n) => (
                      <option key={n} value={n}>
                        {n}
                      </option>
                    ))}
                  </select>
                ) : (
                  <TextCommit value={name} className="mono" label={t('inspector.ports.name', { port: name })} disabled={ro} onCommit={(s) => void p.edits.intend([op({ new_port: s })], where(name))} testId={`port-name-${side}-${name}`} />
                )}
                <select className="input" aria-label={t('inspector.ports.type', { port: name })} disabled={ro || !!allowed} value={port.type} onChange={(e) => void p.edits.intend([op({ type: e.target.value })], where(name))}>
                  {[...new Set([port.type, ...PORT_TYPES])].map((ty) => (
                    <option key={ty} value={ty}>
                      {ty}
                    </option>
                  ))}
                </select>
                <label className="row ins-inline">
                  <input type="checkbox" className="ins-check" disabled={ro} checked={port.required === true} onChange={(e) => void p.edits.apply([op({ required: e.target.checked })], where(name))} />
                  {t('inspector.ports.required')}
                </label>
                <Button size="sm" variant="danger" disabled={ro} onClick={() => void p.edits.intend([{ op: 'remove_port', node: p.nodeId, direction: p.direction, port: name }], where(name))} aria-label={t('inspector.ports.removeLabel', { port: name })} data-testid={`remove-port-${side}-${name}`}>
                  {t('inspector.remove')}
                </Button>
              </div>
              {p.sources?.[name] && <SourceLine sources={p.sources[name]!} required={port.required === true} />}
              {(diags.length > 0 || refusal) && (
                <ul className="ins-diags">
                  {refusal && <li className="err">{t('inspector.refused', { message: refusal })}</li>}
                  {diags.map((d, i) => (
                    <li key={i} className={d.severity === 'error' ? 'err' : 'warn'}>
                      <span aria-hidden="true">{d.severity === 'error' ? '✖ ' : '⚠ '}</span>
                      {d.message}
                    </li>
                  ))}
                </ul>
              )}
            </li>
          )
        })}
      </ul>
      {!ro && <AddPort {...p} free={allowed ? free : undefined} />}
    </div>
  )
}

function SourceLine({ sources, required }: { sources: Source[]; required: boolean }) {
  const { t } = useI18n()
  return (
    <div className="mu ins-source" data-testid="input-source">
      {sources.map((s, i) => (
        <span key={i}>
          {i > 0 && ', '}
          {s.kind === 'edge' ? (
            <span className="mono">{s.from}</span>
          ) : s.kind === 'graph' ? (
            <span>{t('inspector.source.graph', { from: s.from })}</span>
          ) : s.kind === 'literal' ? (
            <span>{t('inspector.source.literal', { key: `config.${s.key}` })}</span>
          ) : (
            <span>{t(required ? 'inspector.source.unboundRequired' : 'inspector.source.unbound')}</span>
          )}
        </span>
      ))}
    </div>
  )
}

function AddPort(p: Props & { free?: NodeTypeDescriptor['outputs'] }) {
  const { t } = useI18n()
  const [name, setName] = useState('')
  const [type, setType] = useState<string>('string')
  const [required, setRequired] = useState(false)
  const side = p.direction === 'input' ? 'inputs' : 'outputs'
  const refusal = p.edits.refusal(`${side}.+`)
  if (p.free) {
    if (p.free.length === 0) return <p className="mu">{t('inspector.ports.allDeclared')}</p>
    return (
      <div className="row ins-row">
      <select
        className="input"
        aria-label={t('inspector.ports.addOutput')}
        value=""
        data-testid={`add-port-${side}`}
        onChange={(e) => {
          const o = p.free!.find((x) => x.name === e.target.value)
          if (o) void p.edits.apply([{ op: 'add_port', node: p.nodeId, direction: p.direction, port: o.name, type: o.type, required: o.required }], `${side}.+`)
        }}
      >
        <option value="">{t('inspector.ports.addOutput')}</option>
        {p.free.map((o) => (
          <option key={o.name} value={o.name}>
            {o.name} ({o.type}){o.requires ? ` — ${t('inspector.rule.requires', { rules: o.requires.map((r) => (r.equals ? `${r.key} = ${r.equals}` : r.key)).join(', ') })}` : ''}
          </option>
        ))}
      </select>
      {refusal && <span className="err">{t('inspector.refused', { message: refusal })}</span>}
      </div>
    )
  }
  return (
    <form
      className="row ins-row"
      data-testid={`add-port-${side}`}
      onSubmit={(e) => {
        e.preventDefault()
        if (!name.trim()) return
        void p.edits.apply([{ op: 'add_port', node: p.nodeId, direction: p.direction, port: name.trim(), type, required }], `${side}.+`).then((ok) => ok && setName(''))
      }}
    >
      <input className="input mono" value={name} onChange={(e) => setName(e.target.value)} aria-label={t('inspector.ports.newName')} placeholder={t('inspector.ports.newName')} data-testid={`add-port-${side}-name`} />
      <select className="input" value={type} onChange={(e) => setType(e.target.value)} aria-label={t('inspector.ports.newType')}>
        {PORT_TYPES.map((ty) => (
          <option key={ty} value={ty}>
            {ty}
          </option>
        ))}
      </select>
      <label className="row ins-inline">
        <input type="checkbox" className="ins-check" checked={required} onChange={(e) => setRequired(e.target.checked)} />
        {t('inspector.ports.required')}
      </label>
      <Button size="sm" type="submit" data-testid={`add-port-${side}-apply`}>
        {t(p.direction === 'input' ? 'inspector.ports.addInput' : 'inspector.ports.addOutputFree')}
      </Button>
      {refusal && <span className="err">{t('inspector.refused', { message: refusal })}</span>}
    </form>
  )
}
