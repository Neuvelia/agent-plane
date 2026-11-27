import { useEffect, useRef, useState, type ReactNode } from 'react'
import { listRuns } from '../api'
import { getCapabilities, type CapabilityReport } from '../capabilities'
import { useI18n } from '../i18n/context'
import { modifierLabel } from './menuKeys'
import { prefersReducedMotion } from './presence'
import { Button } from './primitives'

// F2.5b: the two small windows the profile menu opens, drawn from the design's
// "Shortcuts, About and Soon" artboard. A modal <dialog>: focus stays inside,
// Escape, Close and a press on the backdrop close it, and focus returns to
// whatever opened it.

/** Exit animation length; styles.css `.modal.closing` uses the same value. */
const EXIT_MS = 140

interface ModalProps {
  title: string
  titleId: string
  onClose: () => void
  children: ReactNode
  testId: string
  /** Buttons placed before Close, right-aligned with it. */
  actions?: ReactNode
  wide?: boolean
}

function Modal({ title, titleId, onClose, children, testId, actions, wide = false }: ModalProps) {
  const { t } = useI18n()
  const ref = useRef<HTMLDialogElement>(null)
  const [closing, setClosing] = useState(false)
  useEffect(() => {
    const d = ref.current
    if (d && !d.open && typeof d.showModal === 'function') d.showModal()
    return () => d?.close()
  }, [])
  const close = () => {
    if (closing) return
    setClosing(true)
    window.setTimeout(
      () => {
        ref.current?.close()
        onClose()
      },
      prefersReducedMotion() ? 0 : EXIT_MS,
    )
  }
  return (
    <dialog
      ref={ref}
      className={`modal${wide ? ' wide' : ''}${closing ? ' closing' : ''}`}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault()
        close()
      }}
      // A press on the dialog element itself (not its content) is a press on the backdrop.
      onClick={(e) => e.target === e.currentTarget && close()}
      data-testid={testId}
    >
      <div className="modal-body">
        <h2 id={titleId}>{title}</h2>
        {children}
        <div className="modal-actions">
          {actions}
          <Button onClick={close} data-testid={`${testId}-close`}>
            {t('dialog.close')}
          </Button>
        </div>
      </div>
    </dialog>
  )
}

function platform(): string {
  return typeof navigator === 'undefined' ? '' : navigator.platform || navigator.userAgent
}

function Keys({ keys }: { keys: string[] }) {
  return (
    <span className="ks">
      {keys.map((k) => (
        <kbd key={k}>{k}</kbd>
      ))}
    </span>
  )
}

function Shortcut({ label, keys }: { label: string; keys: string[] }) {
  return (
    <div className="sc">
      <span className="t">{label}</span>
      <Keys keys={keys} />
    </div>
  )
}

/** Only the shortcuts the app has today. Shortcuts are written as text, never icons. */
export function ShortcutsDialog({ onClose }: { onClose: () => void }) {
  const { t } = useI18n()
  const mod = modifierLabel(platform())
  return (
    <Modal title={t('shortcuts.title')} titleId="shortcuts-title" onClose={onClose} testId="shortcuts-dialog" wide>
      <div className="sc-cols">
        <div className="sc-col">
          <section className="sc-grp" aria-labelledby="sc-anywhere">
            <h3 id="sc-anywhere">{t('shortcuts.anywhere')}</h3>
            <Shortcut label={t('shortcuts.settings')} keys={[mod, ',']} />
            <Shortcut label={t('shortcuts.list')} keys={[mod, '/']} />
          </section>
          <section className="sc-grp" aria-labelledby="sc-editor">
            <h3 id="sc-editor">{t('shortcuts.editor')}</h3>
            <Shortcut label={t('shortcuts.save')} keys={[mod, 'S']} />
          </section>
        </div>
        <div className="sc-col">
          <section className="sc-grp" aria-labelledby="sc-menus">
            <h3 id="sc-menus">{t('shortcuts.menus')}</h3>
            <Shortcut label={t('shortcuts.move')} keys={['↑', '↓']} />
            <Shortcut label={t('shortcuts.close')} keys={['Esc']} />
          </section>
        </div>
      </div>
      <p className="modal-note">{t('shortcuts.note')}</p>
    </Modal>
  )
}

type Load<T> = { state: 'loading' } | { state: 'ok'; value: T } | { state: 'failed' }

/** The text "Copy diagnostics" puts on the clipboard: versions and counts only, never a path or a value. */
export function diagnosticsText(appVersion: string, engine: Load<CapabilityReport['engine']>, runs: Load<number>): string {
  const engineText = engine.state === 'ok' ? `${engine.value.version} ${engine.value.os}/${engine.value.arch}` : 'unknown'
  const runsText = runs.state === 'ok' ? String(runs.value) : 'unknown'
  return [`Tarmind ${appVersion || 'unknown'}`, `Engine ${engineText}`, `Runs stored ${runsText}`].join('\n')
}

export function AboutDialog({ appVersion, onClose }: { appVersion: string; onClose: () => void }) {
  const { t } = useI18n()
  const [engine, setEngine] = useState<Load<CapabilityReport['engine']>>({ state: 'loading' })
  const [runs, setRuns] = useState<Load<number>>({ state: 'loading' })
  const [copy, setCopy] = useState<'idle' | 'copied' | 'failed'>('idle')
  useEffect(() => {
    let alive = true
    void getCapabilities().then((r) => alive && setEngine(r.kind === 'ok' ? { state: 'ok', value: r.report.engine } : { state: 'failed' }))
    listRuns().then(
      (list) => alive && setRuns({ state: 'ok', value: list.length }),
      () => alive && setRuns({ state: 'failed' }),
    )
    return () => {
      alive = false
    }
  }, [])
  const doCopy = () => {
    navigator.clipboard.writeText(diagnosticsText(appVersion, engine, runs)).then(
      () => setCopy('copied'),
      () => setCopy('failed'),
    )
  }
  const engineText = engine.state === 'loading' ? t('about.checking') : engine.state === 'failed' ? t('about.unreachable') : `${engine.value.version} · ${engine.value.os}/${engine.value.arch}`
  const runsText = runs.state === 'loading' ? t('about.checking') : runs.state === 'failed' ? t('about.unreachable') : String(runs.value)
  return (
    <Modal
      title={t('about.title')}
      titleId="about-title"
      onClose={onClose}
      testId="about-dialog"
      actions={
        <button type="button" className="btn ghost" onClick={doCopy} data-testid="about-copy">
          {t('about.copy')}
        </button>
      }
    >
      <p className="modal-note">{t('about.local')}</p>
      <dl className="about-kv">
        <div>
          <dt>{t('about.app')}</dt>
          <dd className="mono" data-testid="about-app">
            {appVersion || t('about.unknown')}
          </dd>
        </div>
        <div>
          <dt>{t('about.engine')}</dt>
          <dd className={engine.state === 'ok' ? 'mono' : undefined} data-testid="about-engine">
            {engineText}
          </dd>
        </div>
        <div>
          <dt>{t('about.runs')}</dt>
          <dd data-testid="about-runs">{runsText}</dd>
        </div>
      </dl>
      <p role="status" className={`modal-status ${copy === 'failed' ? 'err' : 'mu'}`} data-testid="about-copy-status">
        {copy === 'copied' ? t('about.copied') : copy === 'failed' ? t('about.copyFailed') : ''}
      </p>
    </Modal>
  )
}
