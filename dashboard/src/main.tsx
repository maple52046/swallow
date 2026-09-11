import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import './index.css'
import { router } from './router'
import { Provider } from './presentation/components/ui/provider'
import { Toaster } from './presentation/components/ui/toaster'
import { AppProvider } from './di/AppProvider'
import { AuthProvider } from './presentation/contexts/AuthContext'

/**
 * Browser composition root for the Swallow operator console.
 *
 * `Provider` owns styling (Chakra system + color mode); the DI container and auth
 * providers sit inside it so screens resolve use cases and the current user, and
 * the router renders the app. `Toaster` is the single global toast host.
 */
createRoot(document.getElementById('root')!).render(
  <Provider>
    <AppProvider>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </AppProvider>
    <Toaster />
  </Provider>,
)
