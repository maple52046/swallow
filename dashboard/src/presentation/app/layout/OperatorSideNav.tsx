import { Box, Flex, Link as ChakraLink, Stack, Text } from '@chakra-ui/react'
import {
  Activity,
  Boxes,
  LayoutDashboard,
  Network,
  Package,
  Server,
  UploadCloud,
  Workflow,
  type LucideIcon,
} from 'lucide-react'
import { Link as RouterLink, useLocation } from 'react-router-dom'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

interface NavEntry {
  label: string
  path: string
  icon: LucideIcon
}

/** Primary operator destinations in workflow order. */
const NAVIGATION: readonly NavEntry[] = [
  { label: 'Overview', path: '/', icon: LayoutDashboard },
  { label: 'Servers', path: '/servers', icon: Server },
  { label: 'Provisioning', path: '/provisioning/deploy', icon: UploadCloud },
  { label: 'Platforms', path: '/platforms', icon: Boxes },
  { label: 'Software', path: '/software', icon: Package },
  { label: 'Workflows', path: '/workflows', icon: Workflow },
  { label: 'Monitoring', path: '/monitoring', icon: Activity },
  { label: 'Infrastructure', path: '/infrastructure/sites', icon: Network },
]

interface OperatorSideNavProps {
  /** Icon-only rail when true; always false in the mobile drawer. */
  collapsed: boolean
  /** Closes the mobile drawer after native router navigation. */
  onNavigate?: () => void
}

/** Keeps a top-level destination active for all of its nested routes. */
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
 * Accessible router link used by both desktop and mobile navigation.
 *
 * The active marker combines `aria-current`, colour, fill, and a leading rule.
 * Collapsed labels move into a tooltip while the anchor keeps its accessible name.
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
      gap="2"
      minH="10"
      position="relative"
      rounded="lg"
      px={collapsed ? '0' : '2'}
      fontSize="sm"
      fontWeight="medium"
      color={active ? 'brand.fg' : 'fg.muted'}
      bg={active ? 'brand.subtle' : 'transparent'}
      _before={active ? {
        content: '""',
        position: 'absolute',
        insetInlineStart: '0',
        top: '25%',
        h: '50%',
        w: '2px',
        rounded: 'full',
        bg: 'brand.solid',
      } : undefined}
      _hover={{
        bg: active ? 'brand.subtle' : 'bg.muted',
        color: active ? 'brand.fg' : 'fg',
        textDecoration: 'none',
        transform: 'translateX(1px)',
      }}
      _focusVisible={{ outline: '2px solid', outlineColor: 'brand.focusRing', outlineOffset: '2px' }}
      transition="background 0.18s ease, color 0.18s ease, transform 0.18s ease"
    >
      <RouterLink to={href} onClick={() => onNavigate?.()}>
        <Box
          as="span"
          display="inline-flex"
          alignItems="center"
          justifyContent="center"
          w="7"
          h="7"
          rounded="md"
          bg={active ? 'brand.muted' : 'transparent'}
          flexShrink="0"
        >
          <Icon size={17} aria-hidden />
        </Box>
        {!collapsed && <span>{entry.label}</span>}
      </RouterLink>
    </ChakraLink>
  )

  return collapsed ? (
    <Tooltip content={entry.label} positioning={{ placement: 'right' }} showArrow>
      {link}
    </Tooltip>
  ) : link
}

/**
 * Scope-preserving primary navigation shared by the desktop rail and mobile drawer.
 *
 * Every destination passes through `scopedHref`, so changing areas never drops the
 * Site query parameter. The rail may collapse visually without changing link order.
 */
export function OperatorSideNav({ collapsed, onNavigate }: OperatorSideNavProps) {
  const location = useLocation()
  const { scopedHref } = useSiteScope()

  return (
    <Flex direction="column" h="100%" w="full" bg="bg.panel" px="2" py="3">
      <Flex
        align="center"
        justify={collapsed ? 'center' : 'flex-start'}
        gap="2.5"
        h="12"
        px={collapsed ? '0' : '2'}
        mb="5"
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
      {!collapsed && (
        <Text px="3" pb="2" color="fg.muted" fontSize="2xs" fontWeight="semibold" letterSpacing="widest" textTransform="uppercase">
          Workspace
        </Text>
      )}
      <Stack as="nav" aria-label="Primary navigation" flex="1" gap="1" overflowY="auto">
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
