import { useEffect, useId, useState } from 'react'
import { LOCALES, LOCALE_LABEL } from './i18n'
import { useI18n } from './i18n/context'
import type { RecordingSettings } from './types'
import { Button, Card, Field, HelpTip, ListRow, SegmentedControl } from './ui/primitives'

const NAME: Record<(typeof LOCALES)[number], string> = { en: 'English', pl: 'Polski' }

/** Names typed by the person, split on spaces and commas; the main process validates them. */
export function parseNames(text: string): string[] {
  return text.split(/[\s,]+/).filter((n) => n !== '')
}

// F2.5a: Settings holds the interface language. F2.3 E6 adds the opt-in
// "Record run values" preference (ADR-0023), held by the main process.
export function Settings() {
  const { t, locale, setLocale } = useI18n()
  return (
    <section className="config">
      <h1>{t('settings.title')}</h1>
      <Card title={t('settings.application')} testId="settings-application">
        <ListRow>
          <span style={{ flex: 1 }}>{t('settings.language')}</span>
          <HelpTip>{t('settings.languageHelp')}</HelpTip>
          <SegmentedControl label={t('settings.language')} value={locale} onChange={setLocale} segments={LOCALES.map((l) => ({ value: l, label: `${LOCALE_LABEL[l]} · ${NAME[l]}` }))} />
        </ListRow>
      </Card>
      <RecordValues />
    </section>
  )
}

type Notice = { kind: 'saved' } | { kind: 'invalid'; name: string } | { kind: 'failed' }

function RecordValues() {
  const { t } = useI18n()
  const checkId = useId()
  const explainId = useId()
  const namesId = useId()
  const [current, setCurrent] = useState<RecordingSettings | null>(null)
  const [loadFailed, setLoadFailed] = useState(false)
  const [enabled, setEnabled] = useState(false)
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
        setEnabled(s.enabled)
        setNames(s.redactEnv.join(' '))
      })
      .catch(() => {
        if (alive) setLoadFailed(true)
      })
    return () => {
      alive = false
    }
  }, [])

  const save = async () => {
    setBusy(true)
    setNotice(null)
    try {
      const result = await window.agentplane.setRecordingSettings({ enabled, redactEnv: parseNames(names) })
      if (result.ok) {
        setCurrent(result.settings)
        setNames(result.settings.redactEnv.join(' '))
        setNotice({ kind: 'saved' })
      } else if (result.code === 'invalid') {
        setNotice({ kind: 'invalid', name: result.name ?? '' })
      } else {
        setNotice({ kind: 'failed' })
      }
    } catch {
      setNotice({ kind: 'failed' })
    } finally {
      setBusy(false)
    }
  }

  const session = !current ? null : current.active ? t('settings.values.sessionOn') : current.missing.length > 0 ? t('settings.values.sessionMissing', { names: current.missing.join(', ') }) : t('settings.values.sessionOff')

  return (
    <Card title={t('settings.values.title')} testId="settings-values">
      {loadFailed ? (
        <p className="err" role="alert" data-testid="values-load-failed">
          {t('settings.values.loadFailed')}
        </p>
      ) : (
        <div className="stack">
          <div className="check">
            <input id={checkId} type="checkbox" checked={enabled} disabled={!current || busy} onChange={(e) => setEnabled(e.target.checked)} aria-describedby={explainId} data-testid="values-record" />
            <label htmlFor={checkId}>{t('settings.values.record')}</label>
          </div>
          <div id={explainId} className="stack">
            <p className="mu">{t('settings.values.explain')}</p>
            <p className="mu">{t('settings.values.redactionWarning')}</p>
          </div>
          <Field label={t('settings.values.redactEnv')} htmlFor={namesId}>
            <input id={namesId} value={names} disabled={!current || busy} onChange={(e) => setNames(e.target.value)} placeholder="GITHUB_TOKEN" spellCheck={false} autoComplete="off" data-testid="values-redact-env" />
          </Field>
          <p className="mu">{t('settings.values.redactEnvHelp')}</p>
          <div className="row">
            <Button variant="primary" onClick={save} disabled={!current || busy} data-testid="values-save">
              {t('settings.values.save')}
            </Button>
          </div>
          {session && (
            <p role="status" data-testid="values-session">
              {session}
            </p>
          )}
          {notice?.kind === 'saved' && (
            <p className="mu" role="status" data-testid="values-saved">
              {t('settings.values.restart')}
            </p>
          )}
          {notice?.kind === 'invalid' && (
            <p className="err" role="alert" data-testid="values-invalid">
              {t('settings.values.invalid', { name: notice.name })}
            </p>
          )}
          {notice?.kind === 'failed' && (
            <p className="err" role="alert" data-testid="values-save-failed">
              {t('settings.values.saveFailed')}
            </p>
          )}
        </div>
      )}
    </Card>
  )
}
