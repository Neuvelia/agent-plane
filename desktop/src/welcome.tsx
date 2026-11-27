import { useCallback, useEffect, useRef, useState } from 'react'
import { checkCapabilities, getCapabilities, type CapabilityResult } from './capabilities'
import { useConnectionReport } from './connection'
import type { DraftMaker } from './editor/TemplatePicker'
import { formatDateTime } from './i18n'
import type { MessageKey } from './i18n'
import { useI18n } from './i18n/context'
import { SETUP_STEPS, setupCards, setupDraft, type CheckState, type SetupStep } from './setup'
import { CapabilityPanel } from './setupCheck'
import { Arrow } from './setupIcons'
import { AgentsStep, GraphStep, StepHead, WelcomeStep } from './setupSteps'
import { SUGGESTED } from './templates'

export { CapabilityPanel, ToolRow, type CapabilityView } from './setupCheck'

// F2.11 welcome screen as the four-step setup of the canvas "Setup, revised":
// Welcome, Check this computer, Agent nodes, First graph. Skip is always in the
// header, the check never blocks Next, and every progress segment jumps to its
// step. Reaching the end, opening a graph or skipping marks setup completed.

export interface WelcomeActions {
  /** Create a new graph: Blank (undefined) or a template's draft. */
  onCreate: (make: DraftMaker | undefined) => void
  onOpen: () => void
  onSkip: () => void
  /** The engine connection is lost; setup hides the global bar, so its header says so instead. */
  lost?: boolean
  /** Step to open on; stories use it, the app starts at the first. */
  initialStep?: SetupStep
}

const LABELS: Record<SetupStep, MessageKey> = { welcome: 'setup.step.welcome', check: 'setup.step.check', agents: 'setup.step.agents', graph: 'setup.step.graph' }
const HINTS: Partial<Record<SetupStep, MessageKey>> = { agents: 'setup.hint.optional', graph: 'setup.hint.graph' }
const CARDS = setupCards()

export function Welcome({ onCreate, onOpen, onSkip, lost = false, initialStep = 'welcome' }: WelcomeActions) {
  const { t } = useI18n()
  const [step, setStep] = useState(SETUP_STEPS.indexOf(initialStep))
  const [choice, setChoice] = useState<string>(SUGGESTED)
  const check = useCapabilities()
  const title = useRef<HTMLHeadingElement>(null)
  const stepHead = useRef<HTMLHeadingElement>(null)
  const moved = useRef(false)
  const name = SETUP_STEPS[step]!

  // The screen opens on its title; each later step change moves focus to that step's heading.
  useEffect(() => {
    if (!moved.current) title.current?.focus({ preventScroll: true })
    else stepHead.current?.focus({ preventScroll: true })
  }, [step])
  const go = (n: number) => {
    moved.current = true
    setStep(Math.min(SETUP_STEPS.length - 1, Math.max(0, n)))
  }

  const draft = setupDraft(choice)
  const create = () => draft.usable && onCreate(draft.make)
  const last = step === SETUP_STEPS.length - 1
  const hint = name === 'check' && check.view.state !== 'loaded' ? 'setup.hint.continue' : HINTS[name]

  return (
    <section className="su" data-testid="welcome" data-step={name}>
      <header className="su-bar">
        <h1 ref={title} tabIndex={-1}>
          {t('setup.title', { app: t('app.name') })}
        </h1>
        <nav className="su-prog" aria-label={t('setup.progress')}>
          {SETUP_STEPS.map((s, i) => (
            <button key={s} type="button" className={i < step ? 'done' : i === step ? 'cur' : ''} aria-label={t('setup.stepLabel', { n: i + 1, label: t(LABELS[s]) })} aria-current={i === step ? 'step' : undefined} title={t(LABELS[s])} onClick={() => go(i)} data-testid={`setup-progress-${s}`}>
              <i>
                <b />
              </i>
            </button>
          ))}
        </nav>
        <span className="su-sp" />
        {lost && (
          <span className="conn bad" role="status" data-testid="setup-connection-lost">
            <span className="dot sm" aria-hidden="true" />
            {t('topbar.lost')}
          </span>
        )}
        <button type="button" className="su-btn ghost" title={t('setup.skipHint')} onClick={onSkip} data-testid="welcome-skip">
          {t('setup.skip')}
        </button>
      </header>
      {/* Keyed by step: a new step mounts fresh, so its entrance plays from the start. */}
      <div className="su-stage" key={name}>
        {name === 'welcome' && <WelcomeStep headRef={stepHead} />}
        {name === 'check' && (
          <div className="su-col" data-testid="setup-step-check">
            <StepHead n={2} title={t('setup.check.title')} headRef={stepHead} />
            <CapabilityPanel view={check.view} announce={check.announce} appVersion={appVersion()} onCheck={check.recheck} onRetry={check.load} />
          </div>
        )}
        {name === 'agents' && <AgentsStep headRef={stepHead} />}
        {name === 'graph' && <GraphStep headRef={stepHead} cards={CARDS} choice={choice} onChoice={setChoice} onOpen={onOpen} />}
      </div>
      <footer className="su-foot">
        {step > 0 && (
          <button type="button" className="su-btn" onClick={() => go(step - 1)} data-testid="setup-back">
            {t('setup.back')}
          </button>
        )}
        <span className="su-sp" />
        {hint && <span className="hint">{t(hint)}</span>}
        {last ? (
          <button type="button" className="su-btn pr" onClick={create} disabled={!draft.usable} data-testid="welcome-template">
            {t('setup.create', { name: choice })}
            <Arrow />
          </button>
        ) : (
          <button type="button" className="su-btn pr" onClick={() => go(step + 1)} data-testid="setup-next">
            {t(step === 0 ? 'setup.next.check' : 'setup.next')}
            <Arrow />
          </button>
        )}
      </footer>
    </section>
  )
}

function appVersion(): string {
  return typeof window.agentplane?.appVersion === 'string' ? window.agentplane.appVersion : ''
}

/** Reads the capability report once on open; "Check again" asks the engine to detect again. */
function useCapabilities() {
  const { t, locale } = useI18n()
  const [view, setView] = useState<CheckState>({ state: 'loading' })
  const [announce, setAnnounce] = useState('')
  const seq = useRef(0)
  useConnectionReport('welcome', view.state === 'disconnected')

  // Only the latest request may settle the view, so a slow first read cannot overwrite a newer check.
  const settle = useCallback(
    (result: CapabilityResult, mine: number) => {
      if (mine !== seq.current) return
      if (result.kind === 'ok') {
        setView({ state: 'loaded', report: result.report, refreshing: false })
        setAnnounce(t('welcome.checkedAt', { time: formatDateTime(locale, result.report.checked_at) }))
      } else if (result.kind === 'failed') setView({ state: 'failed', status: result.status })
      else setView({ state: 'disconnected' })
    },
    [t, locale],
  )

  const load = useCallback(() => {
    const mine = ++seq.current
    setView({ state: 'loading' })
    void getCapabilities().then((r) => settle(r, mine))
  }, [settle])
  useEffect(load, [load])

  const recheck = useCallback(() => {
    const mine = ++seq.current
    setView((v) => (v.state === 'loaded' ? { ...v, refreshing: true } : { state: 'loading' }))
    setAnnounce('')
    void checkCapabilities().then((r) => settle(r, mine))
  }, [settle])

  return { view, announce, load, recheck }
}
