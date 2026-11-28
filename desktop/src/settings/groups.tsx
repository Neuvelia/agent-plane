import { LOCALES } from '../i18n'
import { useI18n, type LocaleChoice } from '../i18n/context'
import { THEME_CHOICES, useTheme, type ThemeChoice } from '../theme'
import { Button } from '../ui/primitives'
import type { CapabilityView } from '../welcome'
import { Group, Row, Segmented, Stepper } from './parts'

const NAME: Record<(typeof LOCALES)[number], string> = { en: 'English', pl: 'Polski' }

/** Values the desktop engine runs with today: `agentplane serve` defaults and the loop default (internal/domain/graph). */
const ENGINE_DEFAULTS = Object.freeze({ maxConcurrency: 2, maxAttempts: 3 })

const THEME_LABEL = { system: 'choice.system', light: 'theme.light', dark: 'theme.dark' } as const satisfies Record<ThemeChoice, string>

interface Ring {
  ring: number | null
  onRingEnd: () => void
}

export function GeneralGroup({ ring, onRingEnd }: Ring) {
  const { t, choice: languageChoice, setChoice: setLanguageChoice } = useI18n()
  const { choice: themeChoice, setChoice: setThemeChoice } = useTheme()
  // F2.5c: the same choices as the profile menu. System follows the operating
  // system; then every catalogue language, Polski first as the design draws it,
  // each name in its own language.
  const ordered = [...LOCALES.filter((l) => l === 'pl'), ...LOCALES.filter((l) => l !== 'pl')]
  const languages = [{ value: 'system' as LocaleChoice, label: t('choice.system'), testId: 'settings-language-system' }, ...ordered.map((l) => ({ value: l as LocaleChoice, label: <span lang={l}>{NAME[l]}</span>, testId: `language-${l}` }))]
  const themes = THEME_CHOICES.map((c) => ({ value: c, label: t(THEME_LABEL[c]), testId: `settings-theme-${c}` }))
  return (
    <Group id="general" title={t('settings.nav.general')} description={t('settings.general.desc')} ring={ring} onRingEnd={onRingEnd} testId="settings-application">
      <Row title={t('settings.language')} hint={t('settings.languageHint')}>
        <Segmented label={t('settings.language')} value={languageChoice} options={languages} onChange={setLanguageChoice} />
      </Row>
      <Row title={t('settings.appearance')} hint={t('settings.appearanceHint')}>
        <Segmented label={t('settings.appearance')} value={themeChoice} options={themes} onChange={setThemeChoice} />
      </Row>
      {/* No folder preference exists and the renderer never sees paths. */}
      <Row title={t('settings.graphFolder')} hint={t('settings.graphFolderHint')} soon testId="settings-graph-folder">
        <Button size="sm" disabled aria-label={`${t('settings.change')}: ${t('settings.graphFolder')}`}>
          {t('settings.change')}
        </Button>
      </Row>
    </Group>
  )
}

// Execution defaults are flags of `agentplane serve` and graph fields; the
// desktop has no setting for them yet, so they show today's values, read only.
export function ExecutionGroup({ ring, onRingEnd }: Ring) {
  const { t } = useI18n()
  return (
    <Group id="execution" title={t('settings.nav.execution')} description={t('settings.execution.desc')} ring={ring} onRingEnd={onRingEnd} testId="settings-execution">
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

export function EngineGroup({ view, lost, ring, onRingEnd }: Ring & { view: CapabilityView; lost: boolean }) {
  const { t } = useI18n()
  // Any HTTP answer means the engine is reachable; an unreachable engine, or the
  // app reporting the connection lost since (`lost`), is "not connected".
  const state = lost || view.state === 'disconnected' ? 'bad' : view.state === 'loading' ? 'checking' : 'ok'
  const label = state === 'ok' ? t('settings.engine.connected') : state === 'checking' ? t('welcome.checking') : t('settings.engine.lost')
  return (
    <Group id="engine" title={t('settings.nav.engine')} ring={ring} onRingEnd={onRingEnd} testId="settings-engine">
      <Row title={t('settings.engine.status')} hint="agentplane serve · 127.0.0.1" hintMono>
        <span className={`st-pill ${state}`} data-testid="engine-status" data-state={state}>
          <span className="dot" aria-hidden="true" />
          {label}
        </span>
      </Row>
      <Row title={t('settings.engine.store')} hint={t('settings.engine.storeHint')} soon testId="settings-store">
        <Button size="sm" disabled aria-label={`${t('settings.engine.showFolder')}: ${t('settings.engine.store')}`}>
          {t('settings.engine.showFolder')}
        </Button>
      </Row>
    </Group>
  )
}
