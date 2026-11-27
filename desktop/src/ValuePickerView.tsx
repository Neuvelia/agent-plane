import { useId } from 'react'
import { useI18n } from './i18n/context'
import { formatDateTime } from './i18n'
import { Button, Tag } from './ui/primitives'
import type { ValuesPicker } from './useRunValues'

// "Show values from run" (F2.3 requirement 16). A view of useValuesPicker: it
// decides nothing about versions, which valuePicker.ts has already resolved.
export function ValuePicker({ picker }: { picker: ValuesPicker }) {
  const { t, locale } = useI18n()
  const id = useId()
  const { runs, resolved, stream } = picker
  const received = stream.values?.valueEvents ?? 0
  const status =
    stream.load === 'unavailable'
      ? t('values.picker.unavailable')
      : stream.load === 'loading'
        ? t('values.picker.loading')
        : stream.load === 'ready'
          ? t('values.picker.recorded', { count: received })
          : ''
  return (
    <div className="val-picker" data-testid="value-picker">
      <label className="lb" htmlFor={id}>
        {t('values.picker.label')}
      </label>
      {runs.length === 0 ? (
        <span className="mu" data-testid="value-picker-none">
          {picker.runsStatus === 'error' ? t('values.picker.listError') : t('values.picker.none')}
        </span>
      ) : (
        <select id={id} value={resolved.run?.runId ?? ''} onChange={(e) => picker.choose(e.target.value || undefined)} data-testid="value-picker-select">
          {!resolved.run && <option value="">{t('values.picker.choose')}</option>}
          {runs.map((r) => (
            <option key={r.runId} value={r.runId}>
              {t('values.picker.option', { time: formatDateTime(locale, r.startedAt), status: r.status })}
              {r.sameVersion ? '' : ` · ${t('values.picker.differentVersion')}`}
            </option>
          ))}
        </select>
      )}
      {runs.length > 0 && !resolved.run && (
        <span className="mu" data-testid="value-picker-no-match">
          {t('values.picker.noSameVersion')}
        </span>
      )}
      {resolved.needsConfirmation && resolved.run && (
        <div className="row" role="group" aria-label={t('values.picker.differentVersion')} data-testid="value-picker-confirm">
          <Tag>{t('values.picker.differentVersion')}</Tag>
          <span className="mu">{t('values.picker.confirmText')}</span>
          <Button size="sm" onClick={() => picker.confirm(resolved.run!.runId)} data-testid="value-picker-confirm-button">
            {t('values.picker.confirm')}
          </Button>
        </div>
      )}
      {resolved.differentVersion && (
        <span className="mu" data-testid="value-picker-different">
          {t('values.picker.shownDifferent')}
        </span>
      )}
      <span role="status" className="mu" data-testid="value-picker-status">
        {status}
      </span>
      {stream.lost && (
        <span className="row" role="alert" data-testid="value-picker-lost">
          <span className="mu">{t('values.picker.lost')}</span>
          <Button size="sm" onClick={stream.reconnect} data-testid="value-picker-reconnect">
            {t('run.reconnect')}
          </Button>
        </span>
      )}
    </div>
  )
}
