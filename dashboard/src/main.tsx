import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import './index.css'
import { router } from './router'
import { Provider } from './presentation/components/ui/provider'
import { Toaster } from './presentation/components/ui/toaster'
import { AppProvider } from './di/AppProvider'
import { AuthProvider } from './presentation/contexts/AuthContext'
import { ExperimentalFeaturesProvider } from './presentation/contexts/ExperimentalFeaturesContext'

/**
 * Browser composition root for the Swallow operator console.
 *
 * `Provider` owns styling (Chakra system + color mode); the DI container and auth
 * providers sit inside it so screens resolve use cases and the current user, and
 * the router renders the app. Experimental feature switches wrap the router so route
 * guards and navigation read the same per-build state. `Toaster` is the single global toast host.
 */
createRoot(document.getElementById('root')!).render(
  <Provider>
    <AppProvider>
      <ExperimentalFeaturesProvider>
        <AuthProvider>
          <RouterProvider router={router} />
        </AuthProvider>
      </ExperimentalFeaturesProvider>
    </AppProvider>
    <Toaster />
  </Provider>,
)
