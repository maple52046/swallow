import { Divider, NavLink, ScrollArea, Stack, Text, ThemeIcon } from '@mantine/core'
import { useLocation, useNavigate } from 'react-router-dom'
import { IconLayoutDashboard, IconServer } from '@tabler/icons-react'

/**
 * Only screens backed by a real endpoint appear here.
 *
 * Clusters, operations, alerts, and metrics all exist in the API but have no screen
 * yet. They are deliberately absent rather than present with mock data: a screen that
 * looks like it works is worse than one that is missing.
 */
interface NavItem {
  label: string
  path: string
  icon: React.ReactNode
}

const NAV: NavItem[] = [
  { label: 'Overview', path: '/', icon: <IconLayoutDashboard size={16} /> },
  { label: 'Servers', path: '/servers', icon: <IconServer size={16} /> },
]

function NavItemLink({ item }: { item: NavItem }) {
  const location = useLocation()
  const navigate = useNavigate()

  const isActive =
    item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path)

  return (
    <NavLink
      label={<Text size="sm">{item.label}</Text>}
      leftSection={
        <ThemeIcon variant="transparent" size="sm" c={isActive ? 'blue' : 'dimmed'}>
          {item.icon}
        </ThemeIcon>
      }
      active={isActive}
      onClick={() => navigate(item.path)}
      styles={{ root: { borderRadius: 6 } }}
    />
  )
}

export function SideNav() {
  return (
    <ScrollArea h="100%" type="scroll">
      <Stack gap={2} p="xs">
        {NAV.map((item) => (
          <NavItemLink key={item.path} item={item} />
        ))}
        <Divider my={4} />
      </Stack>
    </ScrollArea>
  )
}
