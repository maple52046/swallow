import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import '@radix-ui/themes/styles.css'
import './index.css'
import { router } from './router'
import { AppearanceProvider } from './presentation/app/theme/AppearanceProvider'
import { ToastProvider } from './presentation/components/radix/toast/ToastProvider'
import { AppProvider } from './di/AppProvider'
import { AuthProvider } from './presentation/contexts/AuthContext'

// Composition root. AppearanceProvider renders the single Radix <Theme> and owns
// light/dark; ToastProvider is the app-wide transient-message channel (replacing Mantine
// notifications). Auth and the DI container wrap the router beneath the theme so every
// screen has theme, toasts, session, and use cases available.
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppearanceProvider>
      <ToastProvider>
        <AuthProvider>
          <AppProvider>
            <RouterProvider router={router} />
          </AppProvider>
        </AuthProvider>
      </ToastProvider>
    </AppearanceProvider>
  </StrictMode>,
)
