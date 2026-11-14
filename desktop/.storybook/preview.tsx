import type { Preview } from '@storybook/react-vite'
import { useEffect } from 'react'
import { I18nProvider } from '../src/i18n/context'
import { createBridge, type BridgeData } from '../src/storybook/bridge'
import '../src/styles.css'

const preview: Preview = {
  globalTypes: {
    locale: {
      description: 'Interface language',
      toolbar: { title: 'Language', icon: 'globe', items: [{ value: 'en', title: 'English' }, { value: 'pl', title: 'Polski' }], dynamicTitle: true },
    },
  },
  initialGlobals: { locale: 'en' },
  parameters: {
    layout: 'fullscreen',
    backgrounds: { disabled: true },
    options: { storySort: { order: ['UI', 'Screens'] } },
    a11y: { test: 'error' },
  },
  decorators: [
    (Story, ctx) => {
      // Installed during render so children that read window.agentplane on mount find it.
      const data = ctx.parameters.bridge as BridgeData | undefined
      window.agentplane = createBridge(data ?? { runs: [] })
      const locale = ctx.globals.locale === 'pl' ? 'pl' : 'en'
      useEffect(() => {
        document.documentElement.lang = locale
        document.body.style.background = 'var(--bg)'
      }, [locale])
      return (
        <I18nProvider locale={locale}>
          <div style={{ minHeight: '100vh', background: 'var(--bg)', padding: 8 }}>
            <Story />
          </div>
        </I18nProvider>
      )
    },
  ],
}
export default preview
