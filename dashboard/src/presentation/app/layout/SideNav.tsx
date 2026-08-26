import { Flex } from '@radix-ui/themes'
import {
  DashboardIcon,
  DesktopIcon,
  GearIcon,
  LayersIcon,
} from '@radix-ui/react-icons'
import type { ReactNode } from 'react'
import { NavLink } from '@/presentation/components/radix/NavLink'

/**
 * Only screens backed by a real endpoint appear here.
 *
 * Alerts and metrics exist in the API but have no screen yet, and are deliberately absent
 * rather than present with mock data: a screen that looks like it works is worse than one
 * that is missing. Clusters and operations now have real screens.
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
  { label: 'Clusters', to: '/clusters', icon: <LayersIcon /> },
  { label: 'Operations', to: '/operations', icon: <GearIcon /> },
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
