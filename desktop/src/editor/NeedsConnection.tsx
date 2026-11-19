import { useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, Pill } from '../ui/primitives'
import { addOp, targetType } from './bindings'
import { listConnections, type ConnectionAnswer } from './connections'
import { addInputFor } from './graphData'
import { acceptedOptions, visibleOptions, type Hint } from './hints'
import type { Definition } from './types'
import type { GraphDocument } from './useGraphDocument'

// "Needs a connection" (F2.3 requirement 7): the engine's own warnings about an
// unused output and an unbound required input, each with its accepted
// candidates as one-click bindings. Nothing is applied without a click.

interface Props {
  g: GraphDocument
  def: Definition
  hints: Hint[]
  readOnly: boolean
  announce: (message: string) => void
}

export function NeedsConnection({ g, def, hints, readOnly, announce }: Props) {
  const { t } = useI18n()
  const [open, setOpen] = useState<string | null>(null)
  const [answer, setAnswer] = useState<ConnectionAnswer | 'checking' | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const asked = useRef(0)

  const show = async (h: Hint) => {
    if (open === h.key) return setOpen(null)
    const mine = ++asked.current
    setOpen(h.key)
    setShowAll(false)
    setFailure(null)
    setAnswer('checking')
    const a = await listConnections((ops) => g.applyOps(ops, { quiet: true }), h.kind === 'unused-output' ? { from: h.ref } : { to: h.ref })
    if (mine === asked.current) setAnswer(a)
  }

  const connect = async (h: Hint, ref: string) => {
    const [from, to] = h.kind === 'unused-output' ? [h.ref, ref] : [ref, h.ref]
    const r = await g.applyOps([addOp(from, to)], { quiet: true })
    if (r.ok) announce(t('editor.hints.announce.connected', { from, to }))
    else setFailure(r.message)
  }

  const addInput = async (h: Hint) => {
    const r = await g.applyOps(addInputFor(def, h.ref, targetType(def, h.ref)), { quiet: true })
    if (r.ok) announce(t('editor.hints.announce.inputAdded', { to: h.ref }))
    else setFailure(r.message)
  }

  return (
    <section className="ed-hints" aria-label={t('editor.hints.title')} data-testid="needs-connection">
      <h3 className="ins-head">{t('editor.hints.title')}</h3>
      {hints.length === 0 && <p className="mu">{t('editor.hints.none')}</p>}
      <ul className="ed-hint-list">
        {hints.map((h) => {
          const options = open === h.key && answer && answer !== 'checking' && answer.ok ? acceptedOptions(answer.options) : []
          return (
            <li key={h.key} data-testid="hint" data-ref={h.ref} data-kind={h.kind}>
              <div className="row">
                <Pill tone={h.kind === 'unbound-input' ? 'x' : 'w'} small>
                  {h.kind === 'unbound-input' ? `✖ ${t('editor.hints.unboundInput')}` : `⚠ ${t('editor.hints.unusedOutput')}`}
                </Pill>
                <span className="mono">{h.ref}</span>
              </div>
              <p className="mu">{h.message}</p>
              <div className="row">
                <Button size="sm" disabled={readOnly} aria-expanded={open === h.key} aria-label={t('editor.hints.optionsLabel', { ref: h.ref })} onClick={() => void show(h)} data-testid="hint-options">
                  {t('editor.hints.options')}
                </Button>
                {h.kind === 'unbound-input' && (
                  <Button size="sm" disabled={readOnly} aria-label={t('editor.hints.addInputLabel', { ref: h.ref })} onClick={() => void addInput(h)} data-testid="hint-add-input">
                    {t('editor.hints.addInput')}
                  </Button>
                )}
              </div>
              {open === h.key && (
                <div data-testid="hint-candidates">
                  {answer === 'checking' && <p className="mu" role="status">{t('editor.hints.checking')}</p>}
                  {answer && answer !== 'checking' && !answer.ok && <p className="ed-refusal" role="alert">{t('editor.connectTargetsFailed', { message: answer.message })}</p>}
                  {answer && answer !== 'checking' && answer.ok && options.length === 0 && <p className="mu">{t('editor.hints.noOptions')}</p>}
                  {options.length > 0 && (
                    <ul className="ed-targets" aria-label={t('editor.hints.optionsFor', { ref: h.ref })}>
                      {visibleOptions(options, showAll).map((o) => (
                        <li key={o.ref} className="ed-target">
                          <Button size="sm" aria-label={t('editor.hints.connectLabel', h.kind === 'unused-output' ? { from: h.ref, to: o.ref } : { from: o.ref, to: h.ref })} onClick={() => void connect(h, o.ref)} data-testid={`hint-option-${o.ref}`}>
                            <span className="mono">{o.ref}</span>
                          </Button>
                          <span className="mu">{o.type}{o.required ? ` · ${t('editor.bindings.required')}` : ''}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                  {options.length > visibleOptions(options, showAll).length && (
                    <Button size="sm" onClick={() => setShowAll(true)} data-testid="hint-show-all">
                      {t('editor.hints.showAll', { count: options.length })}
                    </Button>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
      {failure && <p className="ed-refusal" role="alert" data-testid="hint-failure">{t('editor.opFailed', { message: failure })}</p>}
    </section>
  )
}
