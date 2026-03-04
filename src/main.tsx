import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MantineProvider, ColorSchemeScript } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import { ModalsProvider } from '@mantine/modals'
import { RouterProvider } from 'react-router-dom'
import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'
import '@mantine/charts/styles.css'
import './index.css'
import { router } from './router'
import { theme, loadColorScheme } from './presentation/app/theme'
import { AppProvider } from './di/AppProvider'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MantineProvider theme={theme} defaultColorScheme={loadColorScheme()}>
      <ColorSchemeScript defaultColorScheme={loadColorScheme()} />
      <ModalsProvider>
        <Notifications position="top-right" />
        <AppProvider>
          <RouterProvider router={router} />
        </AppProvider>
      </ModalsProvider>
    </MantineProvider>
  </StrictMode>,
)
