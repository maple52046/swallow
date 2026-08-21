import { NavLink as RouterNavLink } from 'react-router-dom'
import { Flex, Text } from '@radix-ui/themes'
import type { ReactNode } from 'react'

interface NavLinkProps {
  to: string
  label: string
  icon?: ReactNode
  /** Match only the exact path (used for the root route so it is not always active). */
  end?: boolean
  /** Called after the link is activated, e.g. to close the mobile nav overlay. */
  onNavigate?: () => void
}

/**
 * Sidebar navigation item, replacing Mantine's `NavLink`.
 *
 * Wraps react-router's `NavLink` so active state comes from the router, not hand-rolled
 * path matching, and styles it with Radix theme tokens. The whole row is the link, so it
 * is keyboard-focusable and announced as a link; the active row is marked by react-router
 * with `aria-current="page"` and shown with an accent background (not colour on text
 * alone).
 */
export function NavLink({ to, label, icon, end, onNavigate }: NavLinkProps) {
  return (
    <RouterNavLink
      to={to}
      end={end}
      onClick={onNavigate}
      style={({ isActive }) => ({
        display: 'block',
        textDecoration: 'none',
        borderRadius: 'var(--radius-3)',
        background: isActive ? 'var(--accent-a3)' : 'transparent',
        color: isActive ? 'var(--accent-11)' : 'var(--gray-12)',
      })}
    >
      <Flex align="center" gap="2" px="3" py="2">
        {icon}
        <Text size="2">{label}</Text>
      </Flex>
    </RouterNavLink>
  )
}
