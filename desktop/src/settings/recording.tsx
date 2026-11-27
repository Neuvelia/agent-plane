import { useEffect, useId, useRef, useState } from 'react'
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

/** True when the names typed differ from the saved list (order and spacing aside from parsing). */
export function namesDirty(draft: string, saved: readonly string[]): boolean {
  const typed = parseNames(draft)
  return typed.length !== saved.length || typed.some((n, i) => n !== saved[i])
}

export interface Recording {
  current: RecordingSettings | null
  loadFailed: boolean
  /** The switch position while its save is in flight. */
  pending: boolean | null
  names: string
  setNames: (text: string) => void
  busy: boolean
  notice: Notice | null
  toggle: (enabled: boolean) => void
  saveNames: () => void
}

// F2.3 E6 "Record run values" (ADR-0023), held by the main process. The switch
// saves at once with the saved names and leaves an unsaved names draft as typed;
// the names row saves the draft with the saved switch. The state lives above
// the Settings panes, so a save in flight and a draft survive switching panes.
export function useRecording(): Recording {
  const [current, setCurrent] = useState<RecordingSettings | null>(null)
  const [loadFailed, setLoadFailed] = useState(false)
  const [pending, setPending] = useState<boolean | null>(null)
  const [names, setNames] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<Notice | null>(null)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    window.agentplane
      .recordingSettings()
      .then((s) => {
        if (!alive.current) return
        setCurrent(s)
        setNames(s.redactEnv.join(' '))
      })
      .catch(() => alive.current && setLoadFailed(true))
    return () => {
      alive.current = false
    }
  }, [])

  const save = async (next: { enabled: boolean; redactEnv: string[] }, onSaved: (saved: string[]) => void) => {
    setBusy(true)
    setNotice(null)
    const result = await window.agentplane.setRecordingSettings(next).catch(() => null)
    if (!alive.current) return
    if (result?.ok) {
      setCurrent((c) => (c ? { ...c, ...result.settings } : c))
      onSaved(result.settings.redactEnv)
    }
    setNotice(noticeOf(result))
    setPending(null)
    setBusy(false)
  }

  const toggle = (enabled: boolean) => {
    if (!current || busy) return
    const before = current.redactEnv
    setPending(enabled)
    // A draft equal to the saved names follows the saved list; a different draft stays as typed.
    void save({ enabled, redactEnv: before }, (saved) => setNames((draft) => (namesDirty(draft, before) ? draft : saved.join(' '))))
  }
  const saveNames = () => {
    if (!current || busy) return
    void save({ enabled: current.enabled, redactEnv: parseNames(names) }, (saved) => setNames(saved.join(' ')))
  }
  return { current, loadFailed, pending, names, setNames, busy, notice, toggle, saveNames }
}

// While a save runs the controls are aria-disabled, not disabled, so keyboard
// focus stays on the control that was used; input before the first read is disabled.
export function RecordingGroup({ recording, flash, onFlashEnd }: { recording: Recording; flash: boolean; onFlashEnd: () => void }) {
  const { t } = useI18n()
  const switchId = useId()
  const hintId = useId()
  const namesId = useId()
  const { current, busy, names } = recording
  const checked = recording.pending ?? current?.enabled ?? false
  const dirty = current ? namesDirty(names, current.redactEnv) : false
  return (
    <Group id="recording" title={t('settings.nav.recording')} description={t('settings.recording.desc')} flash={flash} onFlashEnd={onFlashEnd} footer={<RecordingFoot current={current} notice={recording.notice} />} testId="settings-values">
      {recording.loadFailed ? (
        <div className="st-row">
          <p className="err" role="alert" data-testid="values-load-failed">
            {t('settings.values.loadFailed')}
          </p>
        </div>
      ) : (
        <>
          <Row title={<label htmlFor={switchId}>{t('settings.values.record')}</label>} hint={t('settings.values.recordHint')} hintId={hintId}>
            <Switch id={switchId} checked={checked} disabled={!current} aria-disabled={busy || undefined} onChange={(e) => recording.toggle(e.target.checked)} aria-describedby={hintId} data-testid="values-record" />
          </Row>
          <Row title={<label htmlFor={namesId}>{t('settings.values.redactEnv')}</label>} hint={t('settings.values.redactEnvHint')}>
            <div className="st-names">
              <input id={namesId} value={names} disabled={!current} readOnly={busy} onChange={(e) => recording.setNames(e.target.value)} placeholder="GITHUB_TOKEN" spellCheck={false} autoComplete="off" data-testid="values-redact-env" />
              <Button size="sm" onClick={recording.saveNames} disabled={!current} aria-disabled={busy || undefined} data-testid="values-save">
                {t('settings.values.save')}
              </Button>
            </div>
            {dirty && (
              <span className="st-unsaved" role="status" data-testid="values-unsaved">
                {t('settings.values.unsaved')}
              </span>
            )}
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
