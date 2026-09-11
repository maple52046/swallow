import { useCallback, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { OperatorShell } from './OperatorShell'
import { SiteScopeProvider } from '@/presentation/contexts/SiteScopeContext'

const NAV_COLLAPSED_KEY = 'swallow.shell.sidebar-collapsed'

/** Reads the persisted desktop-rail preference; defaults to expanded when unavailable. */
function loadCollapsed(): boolean {
  try {
    return localStorage.getItem(NAV_COLLAPSED_KEY) === 'true'
  } catch {
    return false
  }
}

/**
 * Authenticated route frame. The desktop-rail collapse preference is local browser
 * state, while Site scope stays URL-owned (via `SiteScopeProvider`) so links,
 * reloads, and back/forward navigation remain shareable. The mobile drawer is
 * ephemeral session state that resets on reload.
 */
export function OperatorLayout() {
  const [collapsed, setCollapsed] = useState(loadCollapsed)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  const openMobileNav = useCallback(() => setMobileNavOpen(true), [])
  const closeMobileNav = useCallback(() => setMobileNavOpen(false), [])

  const toggleSidebar = useCallback(() => {
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
        onOpenMobileNav={openMobileNav}
        onToggleSidebar={toggleSidebar}
      >
        <Outlet />
      </OperatorShell>
    </SiteScopeProvider>
  )
}
