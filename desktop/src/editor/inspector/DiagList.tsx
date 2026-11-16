import { useI18n } from '../../i18n/context'
import type { Diagnostic } from '../../types'

/** The engine's diagnostics (and a refused change) for one part of the inspector, as received. */
export function DiagList({ diags, refusal, id }: { diags: Diagnostic[]; refusal?: string; id?: string }) {
  const { t } = useI18n()
  if (diags.length === 0 && !refusal) return null
  return (
    <ul className="ins-diags" id={id} data-testid="inspector-diagnostics">
      {refusal && <li className="err">{t('inspector.refused', { message: refusal })}</li>}
      {diags.map((d, i) => (
        <li key={i} className={d.severity === 'error' ? 'err' : 'warn'}>
          <span aria-hidden="true">{d.severity === 'error' ? '✖ ' : '⚠ '}</span>
          {d.message}
        </li>
      ))}
    </ul>
  )
}
