import { Box, Drawer, Flex, Link as ChakraLink, Portal } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import { OperatorHeader } from './OperatorHeader'
import { OperatorSideNav } from './OperatorSideNav'

interface OperatorShellProps {
  children: ReactNode
  collapsed: boolean
  mobileNavOpen: boolean
  onCloseMobileNav: () => void
  onOpenMobileNav: () => void
  onToggleSidebar: () => void
}

/**
 * Keyboard skip target that jumps straight to the main region. Hidden off-screen
 * until it receives focus, so keyboard users can bypass the nav without cluttering
 * the visual layout.
 */
function SkipToContent() {
  return (
    <ChakraLink
      href="#swallow-main-content"
      position="absolute"
      insetStart="2"
      top="2"
      zIndex="skipLink"
      bg="bg.panel"
      color="fg"
      px="3"
      py="2"
      rounded="md"
      borderWidth="1px"
      borderColor="border"
      transform="translateY(-150%)"
      transition="transform 0.15s ease"
      _focusVisible={{ transform: 'translateY(0)' }}
    >
      Skip to content
    </ChakraLink>
  )
}

/**
 * Authenticated app frame used by every operator route.
 *
 * Renders a persistent left rail on desktop (collapsible to icons) and folds it
 * into a focus-trapped `Drawer` on mobile — the Drawer owns escape/overlay
 * dismissal and focus restoration, so no manual focus management is needed. The
 * main region is the skip-link target and holds the routed screen.
 */
export function OperatorShell({
  children,
  collapsed,
  mobileNavOpen,
  onCloseMobileNav,
  onOpenMobileNav,
  onToggleSidebar,
}: OperatorShellProps) {
  return (
    <Flex minH="100dvh" bg="bg.subtle">
      <SkipToContent />

      <Box
        as="aside"
        display={{ base: 'none', lg: 'block' }}
        w={collapsed ? '16' : '64'}
        flexShrink="0"
        position="sticky"
        top="0"
        h="100dvh"
        borderInlineEndWidth="1px"
        borderColor="border"
        transition="width 0.15s ease"
      >
        <OperatorSideNav collapsed={collapsed} />
      </Box>

      <Flex direction="column" flex="1" minW="0" minH="100dvh">
        <OperatorHeader onToggleSidebar={onToggleSidebar} onOpenMobileNav={onOpenMobileNav} />
        <Box as="main" id="swallow-main-content" tabIndex={-1} flex="1" minW="0" p={{ base: 4, md: 6 }}>
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
            <Drawer.Content maxW="16rem">
              <Drawer.Body p="0">
                <OperatorSideNav collapsed={false} onNavigate={onCloseMobileNav} />
              </Drawer.Body>
            </Drawer.Content>
          </Drawer.Positioner>
        </Portal>
      </Drawer.Root>
    </Flex>
  )
}
