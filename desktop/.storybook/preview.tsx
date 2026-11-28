import type { Preview } from '@storybook/react-vite'
import { useEffect } from 'react'
import { I18nProvider } from '../src/i18n/context'
import { createBridge, type BridgeData } from '../src/storybook/bridge'
import { ThemeProvider } from '../src/theme'
import '../src/styles.css'

const preview: Preview = {
  globalTypes: {
    locale: {
      description: 'Interface language',
      toolbar: { title: 'Language', icon: 'globe', items: [{ value: 'en', title: 'English' }, { value: 'pl', title: 'Polski' }], dynamicTitle: true },
    },
    theme: {
      description: 'Interface theme (F2.5c)',
      toolbar: { title: 'Theme', icon: 'contrast', items: [{ value: 'dark', title: 'Dark' }, { value: 'light', title: 'Light' }], dynamicTitle: true },
    },
  },
  initialGlobals: { locale: 'en', theme: 'dark' },
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
      // Every story starts from the editor's default view and drawer, whatever an earlier story chose.
      try {
        localStorage.removeItem('agentplane.editor.view')
        localStorage.removeItem('agentplane.editor.problems')
      } catch {
        // no storage: the defaults apply anyway
      }
      const locale = ctx.globals.locale === 'pl' ? 'pl' : 'en'
      // A story may pin a theme (the light-theme stories); otherwise the toolbar decides.
      const theme = (ctx.parameters.theme ?? ctx.globals.theme) === 'light' ? 'light' : 'dark'
      useEffect(() => {
        document.documentElement.lang = locale
        document.body.style.background = 'var(--bg)'
      }, [locale])
      return (
        <I18nProvider locale={locale}>
          {/* Keyed by theme: a toolbar change starts the provider from that choice. */}
          <ThemeProvider key={theme} initialChoice={theme}>
            {/* A full-bleed screen (the graph editor) fills the window, as it does in the app. */}
            <div style={ctx.parameters.bleed ? { height: '100vh', display: 'flex', background: 'var(--bg)' } : { minHeight: '100vh', background: 'var(--bg)', padding: 8 }}>
              <Story />
            </div>
          </ThemeProvider>
        </I18nProvider>
      )
    },
  ],
}
export default preview
