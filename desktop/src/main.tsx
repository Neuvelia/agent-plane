import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { I18nProvider } from './i18n/context'
import { applyTheme, initialTheme, ThemeProvider } from './theme'
import './styles.css'

// macOS windows have no native title bar; styles.css reserves room for the traffic lights.
if (/Mac/i.test(navigator.userAgent)) document.documentElement.classList.add('mac')
// Paint the saved theme before the first render, so a light choice never flashes dark (F2.5b).
applyTheme(initialTheme())

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nProvider>
      <ThemeProvider>
        <App />
      </ThemeProvider>
    </I18nProvider>
  </StrictMode>,
)
