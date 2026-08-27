import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import '@patternfly/react-core/dist/styles/base.css'
import './index.css'
import { router } from './router'
import { AppearanceProvider } from './presentation/app/theme/AppearanceProvider'
import { ToastProvider } from './presentation/components/toast/ToastProvider'
import { AppProvider } from './di/AppProvider'
import { AuthProvider } from './presentation/contexts/AuthContext'

/** Browser composition root for the Swallow operator console. */
createRoot(document.getElementById('root')!).render(
  <AppearanceProvider>
    <ToastProvider>
      <AppProvider>
        <AuthProvider>
          <RouterProvider router={router} />
        </AuthProvider>
      </AppProvider>
    </ToastProvider>
  </AppearanceProvider>,
)
