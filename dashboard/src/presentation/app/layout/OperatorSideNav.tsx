import { Box, Flex, Link as ChakraLink, Stack, Text } from '@chakra-ui/react'
import {
  Activity,
  Boxes,
  LayoutDashboard,
  Network,
  Server,
  UploadCloud,
  Workflow,
  type LucideIcon,
} from 'lucide-react'
import { Link as RouterLink, useLocation } from 'react-router-dom'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { Tooltip } from '@/presentation/components/ui/tooltip'

interface NavEntry {
  label: string
  path: string
  icon: LucideIcon
}

/**
 * Primary navigation targets, ordered by operator workflow. Paths are the
 * canonical entry route for each area; active state matches by route prefix so a
 * detail or sub-tab keeps its top-level item highlighted.
 */
const NAVIGATION: readonly NavEntry[] = [
  { label: 'Overview', path: '/', icon: LayoutDashboard },
  { label: 'Servers', path: '/servers', icon: Server },
  { label: 'Provisioning', path: '/provisioning/deploy', icon: UploadCloud },
  { label: 'Platforms', path: '/platforms', icon: Boxes },
  { label: 'Workflows', path: '/workflows', icon: Workflow },
  { label: 'Monitoring', path: '/monitoring', icon: Activity },
  { label: 'Infrastructure', path: '/infrastructure/sites', icon: Network },
]

interface OperatorSideNavProps {
  /** Icon-only rail when true; full labels otherwise. Always false inside the mobile drawer. */
  collapsed: boolean
  /** Fired after a link is chosen so the mobile drawer can close itself. */
  onNavigate?: () => void
}

/** Matches the current route against a nav entry using the same prefix rules the shell has always used. */
function isEntryActive(path: string, pathname: string): boolean {
  if (path === '/') return pathname === '/'
  if (path.startsWith('/provisioning')) return pathname.startsWith('/provisioning')
  if (path.startsWith('/infrastructure')) return pathname.startsWith('/infrastructure')
  return pathname.startsWith(path)
}

interface NavLinkProps {
  entry: NavEntry
  active: boolean
  collapsed: boolean
  href: string
  onNavigate?: () => void
}

/**
 * One navigation link. Renders a real anchor (router-driven) so keyboard and
 * middle-click behave natively; the accent `brand.subtle`/`brand.fg` treatment
 * plus `aria-current="page"` mark the active area without relying on colour alone.
 * When collapsed the label moves into a right-aligned tooltip.
 */
function NavLink({ entry, active, collapsed, href, onNavigate }: NavLinkProps) {
  const Icon = entry.icon
  const link = (
    <ChakraLink
      asChild
      aria-current={active ? 'page' : undefined}
      aria-label={collapsed ? entry.label : undefined}
      display="flex"
      alignItems="center"
      justifyContent={collapsed ? 'center' : 'flex-start'}
      gap="3"
      rounded="md"
      px={collapsed ? '0' : '3'}
      py="2"
      fontSize="sm"
      fontWeight="medium"
      color={active ? 'brand.fg' : 'fg.muted'}
      bg={active ? 'brand.subtle' : 'transparent'}
      _hover={{ bg: active ? 'brand.subtle' : 'bg.muted', color: active ? 'brand.fg' : 'fg', textDecoration: 'none' }}
      _focusVisible={{ outline: '2px solid', outlineColor: 'brand.focusRing', outlineOffset: '2px' }}
    >
      <RouterLink to={href} onClick={() => onNavigate?.()}>
        <Icon size={18} aria-hidden />
        {!collapsed && <span>{entry.label}</span>}
      </RouterLink>
    </ChakraLink>
  )
  return collapsed ? (
    <Tooltip content={entry.label} positioning={{ placement: 'right' }} showArrow>
      {link}
    </Tooltip>
  ) : (
    link
  )
}

/**
 * Scope-preserving side navigation, shared by the desktop rail and the mobile
 * drawer. Links go through `scopedHref` so the active Site query survives
 * navigation, matching the rest of the shell.
 */
export function OperatorSideNav({ collapsed, onNavigate }: OperatorSideNavProps) {
  const location = useLocation()
  const { scopedHref } = useSiteScope()

  return (
    <Flex direction="column" h="100%" w="full" bg="bg.panel">
      <Flex
        align="center"
        justify={collapsed ? 'center' : 'flex-start'}
        gap="2.5"
        h="16"
        px={collapsed ? '0' : '4'}
        borderBottomWidth="1px"
        borderColor="border"
        flexShrink="0"
      >
        <Box color="brand.solid" flexShrink="0">
          <SwallowLogo />
        </Box>
        {!collapsed && (
          <Text fontWeight="bold" fontSize="lg" letterSpacing="tight">
            Swallow
          </Text>
        )}
      </Flex>
      <Stack as="nav" aria-label="Primary navigation" flex="1" gap="1" p="2" overflowY="auto">
        {NAVIGATION.map((entry) => (
          <NavLink
            key={entry.path}
            entry={entry}
            active={isEntryActive(entry.path, location.pathname)}
            collapsed={collapsed}
            href={scopedHref(entry.path)}
            onNavigate={onNavigate}
          />
        ))}
      </Stack>
    </Flex>
  )
}
