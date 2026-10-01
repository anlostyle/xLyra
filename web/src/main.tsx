import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { installAppViewportSize } from '@/lib/app-viewport'
import { installPreloadErrorRecovery } from '@/lib/preload-recovery'
import { useAuthStore } from '@/stores/auth-store'
import { i18nReady } from './locales/i18n'
import './index.css'
import App from './App'

installPreloadErrorRecovery()
installAppViewportSize()
void useAuthStore.getState().initialize()

i18nReady.then(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
