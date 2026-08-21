import { useState } from 'react'
import { Outlet } from 'react-router-dom'
import { AppShell } from '@/presentation/components/radix/AppShell'
import { Header } from './Header'
import { SideNav } from './SideNav'
import { Footer } from './Footer'

/**
 * The authenticated app layout: header, sidebar, footer, and the routed outlet.
 *
 * Owns the mobile nav open/close state (the shell only renders it). The sidebar is a
 * fixed column on desktop and a toggled overlay on mobile; selecting a nav item or
 * tapping the scrim closes the overlay.
 */
export function AppLayout() {
  const [navOpen, setNavOpen] = useState(false)
  const closeNav = () => setNavOpen(false)

  return (
    <AppShell
      navOpen={navOpen}
      onNavClose={closeNav}
      header={<Header onToggleNav={() => setNavOpen((open) => !open)} />}
      navbar={<SideNav onNavigate={closeNav} />}
      footer={<Footer />}
    >
      <Outlet />
    </AppShell>
  )
}
