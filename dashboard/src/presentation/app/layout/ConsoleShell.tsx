import { Box, Drawer, Flex, Link as ChakraLink, Portal } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import { ConsoleHeader } from './ConsoleHeader'
import { ConsoleSideNav } from './ConsoleSideNav'

interface ConsoleShellProps {
  children: ReactNode
  collapsed: boolean
  mobileNavOpen: boolean
  onCloseMobileNav: () => void
  onOpenMobileNav: () => void
  onToggleSidebar: () => void
}

/**
 * Keyboard skip target that jumps straight to the routed workspace.
 *
 * It remains off-screen until focused so keyboard users can bypass the persistent
 * navigation without adding visual chrome to the compact masthead.
 */
function SkipToContent() {
  return (
    <ChakraLink
      href="#swallow-main-content"
      position="absolute"
      insetStart="2"
      top="2"
      zIndex="skipLink"
      bg="bg.raised"
      color="fg"
      px="3"
      py="2"
      rounded="lg"
      borderWidth="1px"
      borderColor="border"
      boxShadow="floating"
      transform="translateY(-150%)"
      transition="transform 0.18s ease"
      _focusVisible={{ transform: 'translateY(0)' }}
    >
      Skip to content
    </ChakraLink>
  )
}

/**
 * Authenticated frame shared by every user route.
 *
 * Desktop navigation is a persistent, preference-backed rail. Mobile navigation
 * uses Chakra's focus-trapped Drawer, which owns escape dismissal and focus
 * restoration. The routed main region remains full-width for dense inventory.
 */
export function ConsoleShell({
  children,
  collapsed,
  mobileNavOpen,
  onCloseMobileNav,
  onOpenMobileNav,
  onToggleSidebar,
}: ConsoleShellProps) {
  // The portalled selection dock lives outside this subtree, so CSS reads this
  // rail state to center the dock in the workspace instead of the full viewport.
  return (
    <Flex
      className="sw-console-shell"
      data-navigation-collapsed={collapsed}
      minH="100dvh"
      bg="transparent"
    >
      <SkipToContent />

      <Box
        as="aside"
        data-testid="desktop-navigation"
        display={{ base: 'none', lg: 'block' }}
        w={collapsed ? '16' : '52'}
        flexShrink="0"
        position="sticky"
        top="0"
        h="100dvh"
        borderInlineEndWidth="1px"
        borderColor="border"
        bg="bg.panel"
        transition="width 0.18s ease"
      >
        <ConsoleSideNav collapsed={collapsed} />
      </Box>

      <Flex direction="column" flex="1" minW="0" minH="100dvh">
        <ConsoleHeader onToggleSidebar={onToggleSidebar} onOpenMobileNav={onOpenMobileNav} />
        <Box
          as="main"
          id="swallow-main-content"
          tabIndex={-1}
          flex="1"
          minW="0"
          p={{ base: 4, md: 6, xl: 8 }}
        >
          {children}
        </Box>
      </Flex>

      <Drawer.Root
        open={mobileNavOpen}
        onOpenChange={(event) => {
          if (!event.open) onCloseMobileNav()
        }}
        placement="start"
        size="xs"
      >
        <Portal>
          <Drawer.Backdrop />
          <Drawer.Positioner>
            <Drawer.Content maxW="16rem" bg="bg.panel">
              <Drawer.Body p="0">
                <ConsoleSideNav collapsed={false} onNavigate={onCloseMobileNav} />
              </Drawer.Body>
            </Drawer.Content>
          </Drawer.Positioner>
        </Portal>
      </Drawer.Root>
    </Flex>
  )
}
