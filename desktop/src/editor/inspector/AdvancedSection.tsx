import { useI18n } from '../../i18n/context'
import { Button } from '../../ui/primitives'
import type { AdvancedEntry, JsonValue, Op } from '../types'
import { DiagList } from './DiagList'
import { parseLoose } from './model'
import { TextCommit } from './TextCommit'
import type { Edits } from './useEdits'

// Config keys the form cannot show (F2.2 requirement 12): unknown to the
// descriptors, or values a control cannot hold. Each shows the value's YAML as
// the sidecar read it from the file. A replacement is entered as JSON (a flow
// value YAML also reads) or as plain text; nothing is changed unless the person
// commits a new value or removes the key.

interface Props {
  entries: AdvancedEntry[]
  readOnly: boolean
  edits: Edits
  /** The refusal key prefix, so loop and node entries keep separate errors. */
  prefix: string
  onSet: (key: string, value: JsonValue | undefined) => Op[]
}

export function AdvancedSection({ entries, readOnly, edits, prefix, onSet }: Props) {
  const { t } = useI18n()
  return (
    <div className="ins-advanced" data-testid={`advanced-${prefix}`}>
      <p className="mu">{t('inspector.advanced.help')}</p>
      <p className="ins-warn" role="note">
        {t('inspector.advanced.secrets')}
      </p>
      {entries.length === 0 && <p className="mu">{t('inspector.advanced.none')}</p>}
      <ul className="ins-port-list">
        {entries.map((a) => {
          const where = `${prefix}.${a.key}`
          return (
            <li key={a.key} className="ins-port" data-testid={`advanced-entry-${a.key}`}>
              <div className="row between">
                <span className="mono">{a.key}</span>
                <span className="mu">{t(a.reason === 'shape' ? 'inspector.advanced.shape' : 'inspector.advanced.unknown')}</span>
              </div>
              <pre className="ins-yaml mono" aria-label={t('inspector.advanced.value', { key: a.key })}>
                {a.yaml}
              </pre>
              {!readOnly && (
                <div className="row ins-row">
                  <TextCommit value="" keepDraft className="mono" placeholder={t('inspector.advanced.replace')} label={t('inspector.advanced.replaceLabel', { key: a.key })} onCommit={(s) => s !== '' && void edits.apply(onSet(a.key, parseLoose(s)), where)} />
                  <Button size="sm" variant="danger" onClick={() => void edits.apply(onSet(a.key, undefined), where)} aria-label={t('inspector.advanced.removeLabel', { key: a.key })} data-testid={`advanced-remove-${a.key}`}>
                    {t('inspector.remove')}
                  </Button>
                </div>
              )}
              <DiagList diags={[]} refusal={edits.refusal(where)} />
            </li>
          )
        })}
      </ul>
    </div>
  )
}
