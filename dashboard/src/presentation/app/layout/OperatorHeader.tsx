import {
  Box,
  Button,
  Flex,
  HStack,
  IconButton,
  Menu,
  Portal,
  Spacer,
  Text,
} from '@chakra-ui/react'
import { useState } from 'react'
import {
  Check,
  FlaskConical,
  KeyRound,
  KeySquare,
  LogOut,
  MapPin,
  Menu as MenuIcon,
  Monitor,
  Moon,
  PanelLeft,
  Sun,
} from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import type { AppearanceMode } from '@/presentation/app/theme'
import { useAppearance } from '@/presentation/app/theme/appearanceContext'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useExperimentalFeatures } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { ExperimentalFeaturesDialog } from './ExperimentalFeaturesDialog'

interface OperatorHeaderProps {
  /** Collapses or expands the persisted desktop navigation rail. */
  onToggleSidebar: () => void
  /** Opens the focus-trapped mobile navigation drawer. */
  onOpenMobileNav: () => void
}

const APPEARANCE_ICON: Record<AppearanceMode, typeof Monitor> = {
  system: Monitor,
  light: Sun,
  dark: Moon,
}

/** Synthetic selection value for the unscoped view; it cannot collide with a Site id. */
const ALL_SITES = '__all__'

/** Returns compact initials for the visual avatar; the account button still owns the accessible label. */
function accountInitials(displayName: string, username: string): string {
  const initials = displayName
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join('')
  return initials || username.slice(0, 2).toUpperCase() || 'U'
}

/**
 * Sticky console masthead for viewport-level controls.
 *
 * Site scope remains URL-owned, appearance remains browser-owned, and account
 * actions (SSH keys, API keys, sign out) remain session-owned. Compact outlined controls keep those global
 * choices distinct from page actions without consuming a second toolbar row. Development builds
 * add an "Experimental features" account entry; release builds never render it or its dialog.
 */
export function OperatorHeader({ onToggleSidebar, onOpenMobileNav }: OperatorHeaderProps) {
  const navigate = useNavigate()
  const { mode, setMode } = useAppearance()
  const { currentUser, logout } = useAuth()
  const { sites, siteId, setSite, scopedHref } = useSiteScope()
  // The experimental features entry exists only in development builds (adjustable settings).
  const { adjustable: experimentsAdjustable } = useExperimentalFeatures()
  const [experimentsOpen, setExperimentsOpen] = useState(false)

  const selectedSite = sites.find((site) => site.id === siteId)?.name ?? 'All sites'
  const AppearanceIcon = APPEARANCE_ICON[mode]

  const signOut = () => {
    logout()
    navigate('/login', { replace: true })
  }

  return (
    <Flex
      as="header"
      data-testid="operator-header"
      align="center"
      gap="2"
      minH="14"
      px={{ base: 3, md: 4 }}
      flexShrink="0"
      position="sticky"
      top="0"
      zIndex="docked"
      bg="color-mix(in srgb, var(--chakra-colors-bg-panel) 88%, transparent)"
      backdropFilter="blur(14px)"
      borderBottomWidth="1px"
      borderColor="border"
    >
      <IconButton
        aria-label="Open navigation"
        variant="ghost"
        size="sm"
        display={{ base: 'inline-flex', lg: 'none' }}
        onClick={onOpenMobileNav}
      >
        <MenuIcon size={18} />
      </IconButton>
      <IconButton
        aria-label="Toggle navigation"
        variant="ghost"
        size="sm"
        display={{ base: 'none', lg: 'inline-flex' }}
        onClick={onToggleSidebar}
      >
        <PanelLeft size={18} />
      </IconButton>
      <HStack gap="2" display={{ base: 'flex', lg: 'none' }} color="brand.solid">
        <SwallowLogo />
        <Text fontWeight="bold" color="fg" letterSpacing="tight">
          Swallow
        </Text>
      </HStack>

      <Spacer />

      <HStack gap={{ base: '1', md: '2' }}>
        <Menu.Root
          positioning={{ placement: 'bottom-end' }}
          onSelect={(details) => setSite(details.value === ALL_SITES ? undefined : details.value)}
        >
          <Menu.Trigger asChild>
            <Button variant="outline" size="sm" rounded="lg" bg="bg.panel" aria-label={`Site scope: ${selectedSite}`}>
              <MapPin size={16} />
              <Text as="span" display={{ base: 'none', md: 'inline' }} maxW="40" truncate>
                {selectedSite}
              </Text>
            </Button>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content minW="12rem" maxH="20rem" overflowY="auto" boxShadow="floating">
                <Menu.Item value={ALL_SITES}>
                  <Text flex="1">All sites</Text>
                  {siteId === undefined && <Check size={16} />}
                </Menu.Item>
                {sites.map((site) => (
                  <Menu.Item key={site.id} value={site.id}>
                    <Text flex="1">{site.name}</Text>
                    {siteId === site.id && <Check size={16} />}
                  </Menu.Item>
                ))}
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>

        <Menu.Root
          positioning={{ placement: 'bottom-end' }}
          onSelect={(details) => setMode(details.value as AppearanceMode)}
        >
          <Menu.Trigger asChild>
            <IconButton variant="outline" size="sm" rounded="lg" bg="bg.panel" aria-label={`Appearance: ${mode}`}>
              <AppearanceIcon size={16} />
            </IconButton>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content minW="10rem" boxShadow="floating">
                <Menu.Item value="system">
                  <Monitor size={16} />
                  <Text flex="1">System</Text>
                  {mode === 'system' && <Check size={16} />}
                </Menu.Item>
                <Menu.Item value="light">
                  <Sun size={16} />
                  <Text flex="1">Light</Text>
                  {mode === 'light' && <Check size={16} />}
                </Menu.Item>
                <Menu.Item value="dark">
                  <Moon size={16} />
                  <Text flex="1">Dark</Text>
                  {mode === 'dark' && <Check size={16} />}
                </Menu.Item>
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>

        {currentUser && (
          <Menu.Root
            positioning={{ placement: 'bottom-end' }}
            onSelect={(details) => {
              if (details.value === 'signout') signOut()
              // SSH keys are account settings, not Site data; the scope is kept only so returning
              // to a fleet page lands in the same Site.
              if (details.value === 'ssh-keys') navigate(scopedHref('/account/ssh-keys'))
              if (details.value === 'api-keys') navigate(scopedHref('/account/api-keys'))
              if (details.value === 'experimental-features') setExperimentsOpen(true)
            }}
          >
            <Menu.Trigger asChild>
              <Button variant="outline" size="sm" rounded="lg" bg="bg.panel" aria-label="Account menu">
                <Box
                  as="span"
                  display="inline-flex"
                  alignItems="center"
                  justifyContent="center"
                  w="6"
                  h="6"
                  rounded="md"
                  bg="brand.subtle"
                  color="brand.fg"
                  fontSize="2xs"
                  fontWeight="bold"
                  aria-hidden
                >
                  {accountInitials(currentUser.displayName, currentUser.username)}
                </Box>
                <Text as="span" display={{ base: 'none', md: 'inline' }} maxW="40" truncate>
                  {currentUser.displayName}
                </Text>
              </Button>
            </Menu.Trigger>
            <Portal>
              <Menu.Positioner>
                <Menu.Content minW="14rem" boxShadow="floating">
                  <Menu.ItemGroup>
                    <Menu.ItemGroupLabel>
                      <Flex direction="column" gap="0.5">
                        <Text fontWeight="medium" color="fg">
                          {currentUser.username}
                        </Text>
                        <Text fontSize="xs" color="fg.muted" textTransform="capitalize">
                          {currentUser.role}
                        </Text>
                      </Flex>
                    </Menu.ItemGroupLabel>
                  </Menu.ItemGroup>
                  <Menu.Separator />
                  <Menu.Item value="ssh-keys">
                    <KeyRound size={16} />
                    <Text flex="1">SSH keys</Text>
                  </Menu.Item>
                  <Menu.Item value="api-keys">
                    <KeySquare size={16} />
                    <Text flex="1">API keys</Text>
                  </Menu.Item>
                  {experimentsAdjustable && (
                    <Menu.Item value="experimental-features">
                      <FlaskConical size={16} />
                      <Text flex="1">Experimental features</Text>
                    </Menu.Item>
                  )}
                  <Menu.Item value="signout">
                    <LogOut size={16} />
                    <Text flex="1">Sign out</Text>
                  </Menu.Item>
                </Menu.Content>
              </Menu.Positioner>
            </Portal>
          </Menu.Root>
        )}
        {experimentsAdjustable && (
          <ExperimentalFeaturesDialog open={experimentsOpen} onClose={() => setExperimentsOpen(false)} />
        )}
      </HStack>
    </Flex>
  )
}
