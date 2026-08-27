import { createRef, useMemo } from 'react'
import { Nav, NavItem, NavList, Tooltip } from '@patternfly/react-core'
import {
  ChartLineIcon,
  CubesIcon,
  ServerIcon,
  TachometerAltIcon,
  TasksIcon,
} from '@patternfly/react-icons'
import { useLocation, useNavigate } from 'react-router-dom'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const NAVIGATION = [
  { label: 'Overview', path: '/', icon: <TachometerAltIcon /> },
  { label: 'Servers', path: '/servers', icon: <ServerIcon /> },
  { label: 'Clusters', path: '/clusters', icon: <CubesIcon /> },
  { label: 'Operations', path: '/operations', icon: <TasksIcon /> },
  { label: 'Monitoring', path: '/monitoring', icon: <ChartLineIcon /> },
] as const

interface OperatorSideNavProps {
  collapsed: boolean
  onNavigate?: () => void
}

/**
 * Scope-preserving PatternFly docked navigation.
 *
 * When collapsed PatternFly hides link text and each anchor receives a tooltip, leaving
 * familiar icons rather than truncated text links. Active state follows route prefixes.
 */
export function OperatorSideNav({ collapsed, onNavigate }: OperatorSideNavProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const refs = useMemo(
    () => NAVIGATION.map(() => createRef<HTMLAnchorElement>()),
    [],
  )

  return (
    <>
      <Nav variant="docked" isTextExpanded={!collapsed} aria-label="Primary navigation">
        <NavList>
          {NAVIGATION.map((item, index) => {
            const active = item.path === '/'
              ? location.pathname === '/'
              : location.pathname.startsWith(item.path)
            const href = scopedHref(item.path)
            return (
              <NavItem
                key={item.path}
                to={href}
                icon={item.icon}
                isActive={active}
                anchorRef={refs[index]}
                aria-label={item.label}
                onClick={(event) => {
                  event.preventDefault()
                  navigate(href)
                  onNavigate?.()
                }}
              >
                {item.label}
              </NavItem>
            )
          })}
        </NavList>
      </Nav>
      {collapsed && NAVIGATION.map((item, index) => (
        <Tooltip
          key={item.path}
          aria="none"
          aria-live="off"
          triggerRef={refs[index]}
          content={item.label}
          position="right"
        />
      ))}
    </>
  )
}
