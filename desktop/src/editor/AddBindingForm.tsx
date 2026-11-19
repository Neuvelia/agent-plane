import { useRef, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import { addOp, providersOf, replaceOps, sourceChoices, type Binding } from './bindings'
import { listConnections, type ConnectionAnswer } from './connections'
import { TargetOptions } from './TargetOptions'
import type { ConnectionOption, Definition } from './types'
import type { GraphDocument } from './useGraphDocument'

// "Add binding" (F2.3 requirement 4): choose a source, then pick among the
// engine's answers. Replace removes the old provider and adds the new one in one
// atomic batch; the engine refuses the batch as a whole, so a failed replace
// keeps the old provider. Inline, no focus trap, Escape cancels.

export interface Changed {
  announce: string
  focus: { from: string; to: string }
}

interface Replacing {
  to: string
  removing: Binding[]
  /** Set when the engine refused the replacement; no confirm button then. */
  refusal?: string
}

interface Props {
  g: GraphDocument
  def: Definition
  onChanged: (c: Changed) => void
  onCancel: () => void
}

export function AddBindingForm({ g, def, onChanged, onCancel }: Props) {
  const { t } = useI18n()
  const [source, setSource] = useState('')
  const [answer, setAnswer] = useState<ConnectionAnswer | 'checking' | null>(null)
  const [refusal, setRefusal] = useState<string | null>(null)
  const [replacing, setReplacing] = useState<Replacing | null>(null)
  const asked = useRef(0)
  const choices = sourceChoices(def)

  const choose = async (ref: string) => {
    setSource(ref)
    setRefusal(null)
    setReplacing(null)
    if (!ref) return setAnswer(null)
    const mine = ++asked.current
    setAnswer('checking')
    const a = await listConnections((ops) => g.applyOps(ops, { quiet: true }), { from: ref })
    if (mine === asked.current) setAnswer(a)
  }

  const connect = async (to: string) => {
    const r = await g.applyOps([addOp(source, to)], { quiet: true })
    if (r.ok) onChanged({ announce: t('editor.bindings.announce.added', { from: source, to }), focus: { from: source, to } })
    else setRefusal(r.message)
  }

  // Ask the engine first: a replacement it refuses (another type, a cycle) is explained instead of offered.
  const askReplace = async (to: string) => {
    const ops = replaceOps(def, source, to)
    if (!ops) return
    const preview = await g.previewOps(ops)
    setReplacing({ to, removing: providersOf(def, to), refusal: preview.ok ? undefined : preview.message })
  }

  const confirmReplace = async () => {
    const ops = replacing && replaceOps(def, source, replacing.to)
    if (!replacing || !ops) return
    const r = await g.applyOps(ops, { quiet: true })
    if (r.ok) {
      const old = replacing.removing.map((b) => b.from).join(', ')
      onChanged({ announce: t('editor.bindings.announce.replaced', { old, from: source, to: replacing.to }), focus: { from: source, to: replacing.to } })
    } else setReplacing({ ...replacing, refusal: r.message })
  }

  const replaceControl = (o: ConnectionOption) => {
    const removing = providersOf(def, o.ref)
    // Nothing to replace on a free input, and replacing a provider with itself changes nothing.
    if (removing.length === 0 || removing.some((b) => b.from === source)) return null
    return (
      <Button size="sm" onClick={() => void askReplace(o.ref)} aria-label={t('editor.bindings.replaceLabel', { to: o.ref })} data-testid={`replace-${o.ref}`}>
        {t('editor.bindings.replace')}
      </Button>
    )
  }

  return (
    <fieldset
      className="ed-add"
      data-testid="add-binding"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation()
          onCancel()
        }
      }}
    >
      <legend className="sr-only">{t('editor.bindings.addTitle')}</legend>
      <div className="row">
        <label htmlFor="binding-source">{t('editor.bindings.source')}</label>
        <select id="binding-source" className="input mono" value={source} onChange={(e) => void choose(e.target.value)} autoFocus data-testid="binding-source">
          <option value="">{t('editor.bindings.sourcePlaceholder')}</option>
          {choices.map((c) => (
            <option key={c.ref} value={c.ref}>
              {c.ref} ({c.type})
            </option>
          ))}
        </select>
        <Button size="sm" onClick={onCancel} data-testid="add-binding-cancel">
          {t('editor.cancel')}
        </Button>
      </div>
      {choices.length === 0 && <p className="mu">{t('editor.bindings.noSources')}</p>}
      {answer === 'checking' && <p className="mu" role="status">{t('editor.connect.checking')}</p>}
      {answer && answer !== 'checking' && !answer.ok && (
        <p className="ed-refusal" role="alert">
          {t('editor.connectTargetsFailed', { message: answer.message })}
        </p>
      )}
      {answer && answer !== 'checking' && answer.ok && answer.options.length === 0 && <p className="mu">{t('editor.connect.noTargets')}</p>}
      {answer && answer !== 'checking' && answer.ok && answer.options.length > 0 && <TargetOptions options={answer.options} label={t('editor.bindings.targetsLabel', { from: source })} onPick={(to) => void connect(to)} extra={replaceControl} />}
      {refusal && (
        <p className="ed-refusal" role="alert" data-testid="connect-refusal">
          {t('editor.cannotConnect', { message: refusal })}
        </p>
      )}
      {replacing && (
        <div className="row ed-confirm" role="group" aria-label={t('editor.bindings.replaceTitle')} data-testid="replace-confirm">
          {replacing.refusal ? (
            <span className="ed-refusal">{t('editor.bindings.replaceRefused', { message: replacing.refusal })}</span>
          ) : (
            <>
              <span>{t('editor.bindings.replaceConfirm', { removed: replacing.removing.map((b) => `${b.from} → ${b.to}`).join(', '), added: `${source} → ${replacing.to}` })}</span>
              <Button size="sm" variant="primary" onClick={() => void confirmReplace()} data-testid="replace-apply">
                {t('editor.bindings.replaceApply')}
              </Button>
            </>
          )}
          <Button size="sm" onClick={() => setReplacing(null)} data-testid="replace-cancel">
            {t('editor.cancel')}
          </Button>
        </div>
      )}
    </fieldset>
  )
}
