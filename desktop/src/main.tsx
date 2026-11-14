import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { I18nProvider } from './i18n/context'
import './styles.css'

// macOS windows have no native title bar; styles.css reserves room for the traffic lights.
if (/Mac/i.test(navigator.userAgent)) document.documentElement.classList.add('mac')

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </StrictMode>,
)
