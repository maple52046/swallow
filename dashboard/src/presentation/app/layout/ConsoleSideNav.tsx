import { Box, Flex, Link as ChakraLink, Separator, Stack, Text } from '@chakra-ui/react'
import {
  Activity,
  Boxes,
  ChartLine,
  Disc3,
  LayoutDashboard,
  Network,
  Package,
  Server,
  type LucideIcon,
} from 'lucide-react'
import { Fragment } from 'react'
import { Link as RouterLink, useLocation } from 'react-router-dom'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useExperimentalFeatures } from '@/presentation/contexts/ExperimentalFeaturesContext'
import type { ExperimentalFeature } from '@/application/ports/ExperimentalFeatureSettings'

/**
 * One sidebar destination. `path` is where the link goes; `activeFor` lists the route prefixes
 * that mark it as the current area, so an entry stays active on its nested and sibling routes
 * (Activity on both `/workflows` and `/change-plans`). Prefixes match whole path segments, and
 * the root prefix `/` matches only the root itself.
 */
interface NavEntry {
  label: string
  path: string
  icon: LucideIcon
  activeFor: readonly string[]
  /** In-development feature the destination belongs to; listed only while it is shown. */
  feature?: ExperimentalFeature
}

/**
 * Primary destinations in workflow order, in sections the sidebar separates with an unlabelled
 * rule: the Overview; the fleet and what runs on it; what happens to it; what the Site is set up
 * with, the OS images it deploys and its wiring.
 */
const NAVIGATION: ReadonlyArray<readonly NavEntry[]> = [
  [{ label: 'Overview', path: '/', icon: LayoutDashboard, activeFor: ['/'] }],
  [
    { label: 'Servers', path: '/servers', icon: Server, activeFor: ['/servers'] },
    { label: 'Platforms', path: '/platforms', icon: Boxes, activeFor: ['/platforms'] },
    { label: 'Software', path: '/software', icon: Package, activeFor: ['/software'] },
  ],
  [
    { label: 'Monitoring', path: '/monitoring', icon: ChartLine, activeFor: ['/monitoring'] },
    { label: 'Activity', path: '/workflows', icon: Activity, activeFor: ['/workflows', '/change-plans'] },
  ],
  [
    { label: 'OS Images', path: '/provisioning/images', icon: Disc3, activeFor: ['/provisioning'] },
    { label: 'Infrastructure', path: '/infrastructure/sites', icon: Network, activeFor: ['/infrastructure'] },
  ],
]

interface ConsoleSideNavProps {
  /** Icon-only rail when true; always false in the mobile drawer. */
  collapsed: boolean
  /** Closes the mobile drawer after native router navigation. */
  onNavigate?: () => void
}

/** Whether `pathname` is inside `entry`'s area (see `NavEntry.activeFor`). */
function isEntryActive(entry: NavEntry, pathname: string): boolean {
  return entry.activeFor.some((prefix) =>
    prefix === '/' ? pathname === '/' : pathname === prefix || pathname.startsWith(`${prefix}/`),
  )
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
 * Destinations of in-development features are omitted while they are hidden; their
 * routes are guarded separately, so this only keeps navigation honest.
 *
 * Assistive technology gets one `nav` holding one flat list: the rules between sections
 * are hidden list items, so they neither count as entries nor split the list.
 */
export function ConsoleSideNav({ collapsed, onNavigate }: ConsoleSideNavProps) {
  const location = useLocation()
  const { scopedHref } = useSiteScope()
  const { enabled: features } = useExperimentalFeatures()
  const shown = (entry: NavEntry) => !entry.feature || features[entry.feature]
  const sections = NAVIGATION
    .map((section) => section.filter(shown))
    .filter((section) => section.length > 0)

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
      <Box as="nav" aria-label="Primary navigation" flex="1" overflowY="auto">
        <Stack as="ul" gap="1">
          {sections.map((section, index) => (
            <Fragment key={section[0].path}>
              {index > 0 && (
                <Box as="li" aria-hidden="true" py="2" px={collapsed ? '1' : '2'}>
                  <Separator />
                </Box>
              )}
              {section.map((entry) => (
                <Box as="li" key={entry.path}>
                  <NavLink
                    entry={entry}
                    active={isEntryActive(entry, location.pathname)}
                    collapsed={collapsed}
                    href={scopedHref(entry.path)}
                    onNavigate={onNavigate}
                  />
                </Box>
              ))}
            </Fragment>
          ))}
        </Stack>
      </Box>
    </Flex>
  )
}
