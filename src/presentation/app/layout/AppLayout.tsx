import { useState } from 'react'
import { AppShell } from '@mantine/core'
import { Outlet } from 'react-router-dom'
import { Header } from './Header'
import { SideNav } from './SideNav'
import { Footer } from './Footer'

export function AppLayout() {
  const [opened, setOpened] = useState(false)

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: 240, breakpoint: 'sm', collapsed: { mobile: !opened } }}
      footer={{ height: 36 }}
      padding="md"
    >
      <AppShell.Header>
        <Header opened={opened} toggle={() => setOpened((o) => !o)} />
      </AppShell.Header>

      <AppShell.Navbar>
        <SideNav />
      </AppShell.Navbar>

      <AppShell.Main>
        <Outlet />
      </AppShell.Main>

      <AppShell.Footer>
        <Footer />
      </AppShell.Footer>
    </AppShell>
  )
}
