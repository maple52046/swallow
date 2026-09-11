import {
  Button,
  Flex,
  HStack,
  IconButton,
  Menu,
  Portal,
  Spacer,
  Text,
} from '@chakra-ui/react'
import {
  Check,
  CircleUser,
  LogOut,
  MapPin,
  Menu as MenuIcon,
  Monitor,
  Moon,
  PanelLeft,
  Sun,
} from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useAppearance } from '@/presentation/app/theme/appearanceContext'
import type { AppearanceMode } from '@/presentation/app/theme'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'

interface OperatorHeaderProps {
  /** Collapses/expands the desktop rail (icon-only vs. full labels). */
  onToggleSidebar: () => void
  /** Opens the mobile navigation drawer. */
  onOpenMobileNav: () => void
}

const APPEARANCE_ICON: Record<AppearanceMode, typeof Monitor> = {
  system: Monitor,
  light: Sun,
  dark: Moon,
}

/** The synthetic option value used for the "All sites" scope, distinct from any real Site id. */
const ALL_SITES = '__all__'

/**
 * Console masthead.
 *
 * Owns the viewport-level controls: navigation toggles (mobile drawer + desktop
 * collapse), Site scope, appearance, and the account menu. Site scope is
 * URL-owned, appearance is persisted browser state, and the account menu reads the
 * authenticated user — so the same boundaries hold no matter the viewport.
 */
export function OperatorHeader({ onToggleSidebar, onOpenMobileNav }: OperatorHeaderProps) {
  const navigate = useNavigate()
  const { mode, setMode } = useAppearance()
  const { currentUser, logout } = useAuth()
  const { sites, siteId, setSite } = useSiteScope()

  const selectedSite = sites.find((site) => site.id === siteId)?.name ?? 'All sites'
  const AppearanceIcon = APPEARANCE_ICON[mode]

  const signOut = () => {
    logout()
    navigate('/login', { replace: true })
  }

  return (
    <Flex
      as="header"
      align="center"
      gap="2"
      h="16"
      px={{ base: 3, md: 4 }}
      flexShrink="0"
      position="sticky"
      top="0"
      zIndex="docked"
      bg="bg.panel"
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
            <Button variant="ghost" size="sm" aria-label={`Site scope: ${selectedSite}`}>
              <MapPin size={16} />
              <Text as="span" display={{ base: 'none', md: 'inline' }} maxW="40" truncate>
                {selectedSite}
              </Text>
            </Button>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content minW="12rem" maxH="20rem" overflowY="auto">
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
            <IconButton variant="ghost" size="sm" aria-label={`Appearance: ${mode}`}>
              <AppearanceIcon size={16} />
            </IconButton>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content minW="10rem">
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
            }}
          >
            <Menu.Trigger asChild>
              <Button variant="ghost" size="sm" aria-label="Account menu">
                <CircleUser size={16} />
                <Text as="span" display={{ base: 'none', md: 'inline' }} maxW="40" truncate>
                  {currentUser.displayName}
                </Text>
              </Button>
            </Menu.Trigger>
            <Portal>
              <Menu.Positioner>
                <Menu.Content minW="14rem">
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
                  <Menu.Item value="signout">
                    <LogOut size={16} />
                    <Text flex="1">Sign out</Text>
                  </Menu.Item>
                </Menu.Content>
              </Menu.Positioner>
            </Portal>
          </Menu.Root>
        )}
      </HStack>
    </Flex>
  )
}
