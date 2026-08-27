import { useCallback, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { OperatorShell } from './OperatorShell'
import { SiteScopeProvider } from '@/presentation/contexts/SiteScopeContext'

const NAV_COLLAPSED_KEY = 'swallow.shell.sidebar-collapsed'

function loadCollapsed(): boolean {
  try {
    return localStorage.getItem(NAV_COLLAPSED_KEY) === 'true'
  } catch {
    return false
  }
}

/**
 * Authenticated route frame. Sidebar preference is local browser state while Site scope
 * remains URL-owned so links, reloads, and back/forward navigation stay shareable.
 */
export function OperatorLayout() {
  const [collapsed, setCollapsed] = useState(loadCollapsed)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const closeMobileNav = useCallback(() => setMobileNavOpen(false), [])

  const toggleDesktopNav = useCallback(() => {
    setCollapsed((current) => {
      const next = !current
      try {
        localStorage.setItem(NAV_COLLAPSED_KEY, String(next))
      } catch {
        // Storage can be blocked by browser policy; the session state remains usable.
      }
      return next
    })
  }, [])

  return (
    <SiteScopeProvider>
      <OperatorShell
        collapsed={collapsed}
        mobileNavOpen={mobileNavOpen}
        onCloseMobileNav={closeMobileNav}
        onToggleDesktopNav={toggleDesktopNav}
        onToggleMobileNav={() => setMobileNavOpen((open) => !open)}
      >
        <Outlet />
      </OperatorShell>
    </SiteScopeProvider>
  )
}
