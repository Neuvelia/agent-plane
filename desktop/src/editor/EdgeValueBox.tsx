import { useMemo, useState } from 'react'
import { useI18n } from '../i18n/context'
import { Button, SegmentedControl, Tag } from '../ui/primitives'
import type { RecordedValue } from '../values'
import { carriedBytes, displayText, formatJson, visibleParts } from './valueText'

// The recorded text of one edge value (F2.3 E5b.2). The text is user data, so it
// is only ever a text node: hidden and bidirectional characters are shown as
// \uXXXX marks, there is no markup, link or open action (an artifact_ref is the
// reference string and nothing more), and Copy runs only from an explicit click.

type Write = (text: string) => Promise<void>

/** copyDisplayed writes exactly the displayed text; it reports failure instead of throwing. */
export async function copyDisplayed(text: string, write: Write = (s) => navigator.clipboard.writeText(s)): Promise<boolean> {
  try {
    await write(text)
    return true
  } catch {
    return false
  }
}

/** Type, size, SHA-256 and the truncated and redacted marks: the facts the engine recorded about a value. */
export function ValueFacts({ value }: { value: RecordedValue }) {
  const { t } = useI18n()
  const shown = value.value === undefined ? 0 : carriedBytes(value.value)
  return (
    <div className="ev-facts mu" data-testid="edge-value-facts">
      <Tag>{value.type}</Tag>
      <span data-testid="edge-value-size">{value.truncated && value.value !== undefined ? t('editor.edgeValue.truncatedOf', { shown, total: value.sizeBytes }) : t('editor.edgeValue.size', { count: value.sizeBytes })}</span>
      {value.truncated && <Tag>{t('editor.edgeValue.truncated')}</Tag>}
      {value.redacted && <Tag>{t('editor.edgeValue.redacted')}</Tag>}
      <span className="mono ev-hash">
        {t('editor.edgeValue.sha')} {value.sha256}
      </span>
    </div>
  )
}

export function EdgeValueBox({ value, label, incomplete }: { value: RecordedValue; label: string; incomplete: boolean }) {
  const { t } = useI18n()
  const text = value.value ?? ''
  const formatted = useMemo(() => (value.type === 'json' && !value.truncated ? formatJson(text) : null), [value.type, value.truncated, text])
  const [raw, setRaw] = useState(false)
  const [note, setNote] = useState('')
  const shown = formatted !== null && !raw ? formatted : text
  const parts = useMemo(() => visibleParts(shown), [shown])
  const empty = text === '' || (value.type === 'json' && text.trim() === 'null')
  const copy = () => void copyDisplayed(displayText(shown)).then((ok) => setNote(t(ok ? 'editor.edgeValue.copied' : 'editor.edgeValue.copyFailed')))
  return (
    <div className="ev-value" data-testid="edge-value">
      <ValueFacts value={value} />
      {incomplete && (
        <div data-testid="edge-value-incomplete">
          <Tag>{t('editor.edgeValue.incomplete')}</Tag>
        </div>
      )}
      <div className="row ev-actions">
        {formatted !== null && (
          <SegmentedControl
            label={t('editor.edgeValue.view')}
            value={raw ? 'raw' : 'formatted'}
            onChange={(v) => {
              setRaw(v === 'raw')
              setNote('')
            }}
            segments={[
              { value: 'formatted', label: t('editor.edgeValue.formatted'), testId: 'edge-value-formatted' },
              { value: 'raw', label: t('editor.edgeValue.raw'), testId: 'edge-value-raw' },
            ]}
          />
        )}
        {!empty && (
          <Button size="sm" onClick={copy} data-testid="edge-value-copy">
            {t('editor.edgeValue.copy')}
          </Button>
        )}
        <span className="sr-only" role="status" aria-live="polite" data-testid="edge-value-copy-status">
          {note}
        </span>
        {note && (
          <span className="mu" aria-hidden="true">
            {note}
          </span>
        )}
      </div>
      {value.type === 'artifact_ref' && <p className="mu">{t('editor.edgeValue.refNote')}</p>}
      {empty && (
        <p className="mu" data-testid="edge-value-empty">
          {t('editor.edgeValue.empty')}
        </p>
      )}
      <div role="region" tabIndex={0} aria-label={label} className="ev-region" data-testid="edge-value-region">
        <pre className="ev-text" data-testid="edge-value-text">
          {parts.map((p, i) => (p.mark ? <span key={i} className="ev-mark">{p.text}</span> : p.text))}
        </pre>
      </div>
    </div>
  )
}
