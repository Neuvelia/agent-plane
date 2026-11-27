import { LOCALES } from '../i18n'
import { useI18n } from '../i18n/context'
import { Button } from '../ui/primitives'
import type { CapabilityView } from '../welcome'
import { Group, Row, Segmented, Stepper } from './parts'

const NAME: Record<(typeof LOCALES)[number], string> = { en: 'English', pl: 'Polski' }

/** Values the desktop engine runs with today: `agentplane serve` defaults and the loop default (internal/domain/graph). */
export const ENGINE_DEFAULTS = Object.freeze({ maxConcurrency: 2, maxAttempts: 3 })

interface Flash {
  flash: boolean
  onFlashEnd: () => void
}

export function GeneralGroup({ flash, onFlashEnd }: Flash) {
  const { t, locale, setLocale } = useI18n()
  // The design lists Polski first; the order is fixed, not by locale.
  const options = (['pl', 'en'] as const).map((l) => ({ value: l, label: NAME[l], testId: `language-${l}` }))
  return (
    <Group id="general" title={t('settings.nav.general')} description={t('settings.general.desc')} flash={flash} onFlashEnd={onFlashEnd} testId="settings-application">
      <Row title={t('settings.language')} hint={t('settings.languageHint')}>
        <Segmented label={t('settings.language')} value={locale} options={options} onChange={setLocale} />
      </Row>
      {/* No folder preference exists and the renderer never sees paths. */}
      <Row title={t('settings.graphFolder')} hint={t('settings.graphFolderHint')} soon testId="settings-graph-folder">
        <Button size="sm" disabled>
          {t('settings.change')}
        </Button>
      </Row>
    </Group>
  )
}

// Execution defaults are flags of `agentplane serve` and graph fields; the
// desktop has no setting for them yet, so they show today's values, read only.
export function ExecutionGroup({ flash, onFlashEnd }: Flash) {
  const { t } = useI18n()
  return (
    <Group id="execution" title={t('settings.nav.execution')} description={t('settings.execution.desc')} flash={flash} onFlashEnd={onFlashEnd} testId="settings-execution">
      <Row title={t('settings.concurrency')} hint={t('settings.concurrencyHint')} soon>
        <Stepper value={ENGINE_DEFAULTS.maxConcurrency} min={1} max={16} label={t('settings.concurrency')} testId="settings-concurrency" />
      </Row>
      <Row title={t('settings.retry')} hint={t('settings.retryHint')}>
        <span className="st-pill" data-testid="settings-retry">
          {t('settings.retryValue', { count: ENGINE_DEFAULTS.maxAttempts })}
        </span>
      </Row>
      <Row title={t('settings.workspace')} hint={t('settings.workspaceHint')} soon>
        <Segmented
          label={t('settings.workspace')}
          value="shared"
          disabled
          options={[
            { value: 'shared', label: t('settings.workspace.shared') },
            { value: 'worktree', label: t('settings.workspace.worktree') },
          ]}
        />
      </Row>
    </Group>
  )
}

export function EngineGroup({ view, flash, onFlashEnd }: Flash & { view: CapabilityView }) {
  const { t } = useI18n()
  // Any HTTP answer means the engine is reachable; only an unreachable engine is "not connected".
  const state = view.state === 'loading' ? 'checking' : view.state === 'disconnected' ? 'bad' : 'ok'
  const label = state === 'ok' ? t('settings.engine.connected') : state === 'checking' ? t('welcome.checking') : t('settings.engine.lost')
  return (
    <Group id="engine" title={t('settings.nav.engine')} flash={flash} onFlashEnd={onFlashEnd} testId="settings-engine">
      <Row title={t('settings.engine.status')} hint="agentplane serve · 127.0.0.1" hintMono>
        <span className={`st-pill ${state}`} data-testid="engine-status" data-state={state}>
          <span className="dot" aria-hidden="true" />
          {label}
        </span>
      </Row>
      <Row title={t('settings.engine.store')} hint={t('settings.engine.storeHint')} soon testId="settings-store">
        <Button size="sm" disabled>
          {t('settings.engine.showFolder')}
        </Button>
      </Row>
    </Group>
  )
}
