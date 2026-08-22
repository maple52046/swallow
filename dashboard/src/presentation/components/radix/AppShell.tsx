import { Box, Flex } from '@radix-ui/themes'
import { useMediaQuery } from './useMediaQuery'

/** Below this width the sidebar becomes a toggled overlay instead of a fixed column. */
const MOBILE_QUERY = '(max-width: 768px)'

const HEADER_HEIGHT = 56
const FOOTER_HEIGHT = 36
const NAV_WIDTH = 240

interface AppShellProps {
  header: React.ReactNode
  navbar: React.ReactNode
  footer: React.ReactNode
  children: React.ReactNode
  /** Whether the mobile nav overlay is open. Ignored on desktop, where nav is always shown. */
  navOpen: boolean
  /** Closes the mobile overlay, e.g. after a nav item is chosen or the scrim is tapped. */
  onNavClose: () => void
}

/**
 * The application layout shell, replacing Mantine's `AppShell`.
 *
 * Fixed header and footer bracket a body that is a sidebar plus a scrollable main region.
 * On viewports narrower than `MOBILE_QUERY` the sidebar collapses to an overlay driven by
 * `navOpen`, with a scrim that closes it — the small-screen affordance the previous shell
 * provided through its burger. Layout is presentational only; the open/close state is
 * owned by the caller.
 */
export function AppShell({ header, navbar, footer, children, navOpen, onNavClose }: AppShellProps) {
  const isMobile = useMediaQuery(MOBILE_QUERY)
  const showSidebar = !isMobile || navOpen

  return (
    <Flex direction="column" style={{ minHeight: '100vh' }}>
      <Box
        asChild
        style={{
          height: HEADER_HEIGHT,
          borderBottom: '1px solid var(--gray-a5)',
          position: 'sticky',
          top: 0,
          zIndex: 5,
          background: 'var(--color-background)',
        }}
      >
        <header>{header}</header>
      </Box>

      <Flex style={{ flex: 1, minHeight: 0, position: 'relative' }}>
        {isMobile && navOpen && (
          // Scrim: a tap outside the overlay closes it. Decorative to assistive tech;
          // the nav items themselves remain the reachable controls.
          <Box
            onClick={onNavClose}
            aria-hidden
            style={{
              position: 'fixed',
              inset: `${HEADER_HEIGHT}px 0 0 0`,
              background: 'var(--black-a6)',
              zIndex: 3,
            }}
          />
        )}

        {showSidebar && (
          <Box
            asChild
            style={{
              width: NAV_WIDTH,
              borderRight: '1px solid var(--gray-a5)',
              background: 'var(--color-panel-solid)',
              ...(isMobile
                ? {
                    position: 'fixed',
                    top: HEADER_HEIGHT,
                    bottom: 0,
                    left: 0,
                    zIndex: 4,
                    overflowY: 'auto',
                  }
                : { flexShrink: 0 }),
            }}
          >
            <nav aria-label="Primary">{navbar}</nav>
          </Box>
        )}

        <Box asChild p="4" style={{ flex: 1, minWidth: 0, overflowY: 'auto' }}>
          <main>{children}</main>
        </Box>
      </Flex>

      <Box
        asChild
        style={{
          height: FOOTER_HEIGHT,
          borderTop: '1px solid var(--gray-a5)',
          background: 'var(--color-panel-solid)',
        }}
      >
        <footer>{footer}</footer>
      </Box>
    </Flex>
  )
}
