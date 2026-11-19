import { useId } from 'react'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import type { ConnectionOption } from './types'

// The engine's answer to "what can this port be joined to" (list_connections),
// shown as received. A refused entry stays in the list with the engine's reason
// as visible text; its button is aria-disabled, not removed, so a keyboard user
// can still reach it and hear why. It never sends anything.

export const canSend = (o: ConnectionOption): boolean => o.ok

interface Props {
  options: ConnectionOption[]
  /** The accessible name of the list. */
  label: string
  /** Sends the connection for an accepted option. */
  onPick: (ref: string) => void
  /** Extra controls for a refused option, such as Replace. */
  extra?: (o: ConnectionOption) => React.ReactNode
  readOnly?: boolean
}

export function TargetOptions({ options, label, onPick, extra, readOnly = false }: Props) {
  const { t } = useI18n()
  const uid = useId()
  return (
    <ul className="ed-targets" aria-label={label} data-testid="target-options">
      {options.map((o, i) => {
        const why = !o.ok ? `${uid}-${i}` : undefined
        return (
          <li key={o.ref} className="ed-target" data-testid="target-option" data-ref={o.ref} data-ok={o.ok ? 'true' : 'false'}>
            <Button
              size="sm"
              aria-disabled={!canSend(o) || readOnly ? true : undefined}
              aria-describedby={why}
              aria-label={t('editor.bindings.connectLabel', { to: o.ref })}
              data-testid={`connect-option-${o.ref}`}
              onClick={() => canSend(o) && !readOnly && onPick(o.ref)}
            >
              <span className="mono">{o.ref}</span>
            </Button>
            <span className="mu">
              {o.type}
              {o.required ? ` · ${t('editor.bindings.required')}` : ''}
            </span>
            {!o.ok && (
              <span className="ed-refusal" id={why} data-testid="target-reason">
                <span aria-hidden="true">{'✖ '}</span>
                {o.message}
              </span>
            )}
            {!o.ok && extra?.(o)}
          </li>
        )
      })}
    </ul>
  )
}
