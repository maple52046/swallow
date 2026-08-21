import { Flex } from '@radix-ui/themes'
import { DashboardIcon, DesktopIcon } from '@radix-ui/react-icons'
import type { ReactNode } from 'react'
import { NavLink } from '@/presentation/components/radix/NavLink'

/**
 * Only screens backed by a real endpoint appear here.
 *
 * Clusters, operations, alerts, and metrics all exist in the API but have no screen
 * yet. They are deliberately absent rather than present with mock data: a screen that
 * looks like it works is worse than one that is missing.
 */
interface NavItem {
  label: string
  to: string
  icon: ReactNode
  /** Exact-match the route so a prefix route does not keep the item permanently active. */
  end?: boolean
}

const NAV: NavItem[] = [
  { label: 'Overview', to: '/', icon: <DashboardIcon />, end: true },
  { label: 'Servers', to: '/servers', icon: <DesktopIcon /> },
]

interface SideNavProps {
  /** Called after a nav item is chosen, so the mobile overlay can close itself. */
  onNavigate?: () => void
}

/**
 * The primary sidebar navigation.
 *
 * Active state comes from react-router via the shared `NavLink`, not manual path matching.
 * On mobile this renders inside the shell's overlay; `onNavigate` lets a selection close
 * that overlay.
 */
export function SideNav({ onNavigate }: SideNavProps) {
  return (
    <Flex direction="column" gap="1" p="2">
      {NAV.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          label={item.label}
          icon={item.icon}
          end={item.end}
          onNavigate={onNavigate}
        />
      ))}
    </Flex>
  )
}
