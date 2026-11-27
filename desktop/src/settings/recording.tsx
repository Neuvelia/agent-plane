import { useEffect, useId, useState } from 'react'
import type { MessageKey } from '../i18n'
import { useI18n } from '../i18n/context'
import type { RecordingSaveResult, RecordingSettings } from '../types'
import { Button } from '../ui/primitives'
import { Group, Row, Switch } from './parts'

/** Names typed by the person, split on spaces and commas; the main process validates them. */
export function parseNames(text: string): string[] {
  return text.split(/[\s,]+/).filter((n) => n !== '')
}

type Notice = { kind: 'saved' } | { kind: 'invalid'; name: string } | { kind: 'failed' }

/** What a save result tells the person. A thrown save is a failure, never "saved". */
export function noticeOf(result: RecordingSaveResult | null): Notice {
  if (!result) return { kind: 'failed' }
  if (result.ok) return { kind: 'saved' }
  return result.code === 'invalid' ? { kind: 'invalid', name: result.name ?? '' } : { kind: 'failed' }
}

/** The session line: the running engine keeps its flags until the app restarts (ADR-0023). */
export function sessionKey(s: RecordingSettings): MessageKey {
  if (s.active) return 'settings.values.sessionOn'
  return s.missing.length > 0 ? 'settings.values.sessionMissing' : 'settings.values.sessionOff'
}

// F2.3 E6 "Record run values" (ADR-0023), held by the main process. The switch
// saves at once with the saved names; the names row saves with the saved switch.
// "Record file diffs" has no desktop setting yet (F2.8b): shown, disabled.
export function RecordingGroup({ flash, onFlashEnd }: { flash: boolean; onFlashEnd: () => void }) {
  const { t } = useI18n()
  const switchId = useId()
  const hintId = useId()
  const namesId = useId()
  const [current, setCurrent] = useState<RecordingSettings | null>(null)
  const [loadFailed, setLoadFailed] = useState(false)
  const [pending, setPending] = useState<boolean | null>(null)
  const [names, setNames] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<Notice | null>(null)

  useEffect(() => {
    let alive = true
    window.agentplane
      .recordingSettings()
      .then((s) => {
        if (!alive) return
        setCurrent(s)
        setNames(s.redactEnv.join(' '))
      })
      .catch(() => {
        if (alive) setLoadFailed(true)
      })
    return () => {
      alive = false
    }
  }, [])

  const save = async (next: { enabled: boolean; redactEnv: string[] }) => {
    setBusy(true)
    setNotice(null)
    const result = await window.agentplane.setRecordingSettings(next).catch(() => null)
    if (result?.ok) {
      setCurrent((c) => (c ? { ...c, ...result.settings } : c))
      setNames(result.settings.redactEnv.join(' '))
    }
    setNotice(noticeOf(result))
    setPending(null)
    setBusy(false)
  }

  const toggle = (enabled: boolean) => {
    if (!current) return
    setPending(enabled)
    void save({ enabled, redactEnv: current.redactEnv })
  }
  const saveNames = () => current && void save({ enabled: current.enabled, redactEnv: parseNames(names) })
  const checked = pending ?? current?.enabled ?? false

  return (
    <Group id="recording" title={t('settings.nav.recording')} description={t('settings.recording.desc')} flash={flash} onFlashEnd={onFlashEnd} footer={<RecordingFoot current={current} notice={notice} />} testId="settings-values">
      {loadFailed ? (
        <div className="st-row">
          <p className="err" role="alert" data-testid="values-load-failed">
            {t('settings.values.loadFailed')}
          </p>
        </div>
      ) : (
        <>
          <Row title={<label htmlFor={switchId}>{t('settings.values.record')}</label>} hint={t('settings.values.recordHint')} hintId={hintId}>
            <Switch id={switchId} checked={checked} disabled={!current || busy} onChange={(e) => toggle(e.target.checked)} aria-describedby={hintId} data-testid="values-record" />
          </Row>
          <Row title={<label htmlFor={namesId}>{t('settings.values.redactEnv')}</label>} hint={t('settings.values.redactEnvHint')}>
            <div className="st-names">
              <input id={namesId} value={names} disabled={!current || busy} onChange={(e) => setNames(e.target.value)} placeholder="GITHUB_TOKEN" spellCheck={false} autoComplete="off" data-testid="values-redact-env" />
              <Button size="sm" onClick={saveNames} disabled={!current || busy} data-testid="values-save">
                {t('settings.values.save')}
              </Button>
            </div>
          </Row>
          <Row title={t('settings.diffs.record')} hint={t('settings.diffs.recordHint')} soon testId="settings-diffs">
            <Switch checked={false} disabled aria-label={t('settings.diffs.record')} />
          </Row>
        </>
      )}
    </Group>
  )
}

function RecordingFoot({ current, notice }: { current: RecordingSettings | null; notice: Notice | null }) {
  const { t } = useI18n()
  return (
    <div className="st-foot">
      {current && (
        <p role="status" data-testid="values-session">
          {t(sessionKey(current), { names: current.missing.join(', ') })}
        </p>
      )}
      {notice?.kind === 'saved' && (
        <p className="st-notice" role="status" data-testid="values-saved">
          {t('settings.values.restart')}
        </p>
      )}
      {notice?.kind === 'invalid' && (
        <p className="st-notice err" role="alert" data-testid="values-invalid">
          {t('settings.values.invalid', { name: notice.name })}
        </p>
      )}
      {notice?.kind === 'failed' && (
        <p className="st-notice err" role="alert" data-testid="values-save-failed">
          {t('settings.values.saveFailed')}
        </p>
      )}
      <details className="st-more">
        <summary>{t('settings.values.more')}</summary>
        <div className="st-more-body">
          <p>{t('settings.values.explain')}</p>
          <p>{t('settings.values.redactionWarning')}</p>
          <p>{t('settings.values.redactEnvHelp')}</p>
        </div>
      </details>
    </div>
  )
}
