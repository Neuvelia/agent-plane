import { LOCALES, LOCALE_LABEL } from './i18n'
import { useI18n } from './i18n/context'
import { Card, HelpTip, ListRow, SegmentedControl } from './ui/primitives'

const NAME: Record<(typeof LOCALES)[number], string> = { en: 'English', pl: 'Polski' }

// F2.5a: Settings holds only the interface language for now. Execution policy,
// effective configuration and privacy/sync belong to later features.
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
    </section>
  )
}
