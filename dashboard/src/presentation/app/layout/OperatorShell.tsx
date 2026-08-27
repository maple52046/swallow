import { useEffect, useRef, type ReactNode } from 'react'
import { Page, PageSection, SkipToContent } from '@patternfly/react-core'
import { OperatorHeader } from './OperatorHeader'
import { OperatorSideNav } from './OperatorSideNav'
import './operator-shell.css'

interface OperatorShellProps {
  children: ReactNode
  collapsed: boolean
  mobileNavOpen: boolean
  onCloseMobileNav: () => void
  onToggleDesktopNav: () => void
  onToggleMobileNav: () => void
}

/** Stock PatternFly docked page used by every authenticated route. */
export function OperatorShell({ children, collapsed, mobileNavOpen, onCloseMobileNav, onToggleDesktopNav, onToggleMobileNav }: OperatorShellProps) {
  const previousFocus = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!mobileNavOpen) return
    previousFocus.current = document.activeElement as HTMLElement | null
    const dock = document.querySelector<HTMLElement>('.sw-operator-shell .pf-v6-c-page__dock')
    const focusable = () => [...(dock?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), select:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? [])].filter((element) => element.offsetParent !== null)
    const focusTimer = window.setTimeout(() => focusable()[0]?.focus(), 0)
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { onCloseMobileNav(); return }
      if (event.key !== 'Tab') return
      const items = focusable()
      if (!items.length) return
      const first = items[0]; const last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      window.clearTimeout(focusTimer)
      document.removeEventListener('keydown', onKeyDown)
      previousFocus.current?.focus()
    }
  }, [mobileNavOpen, onCloseMobileNav])

  const navigation = <OperatorSideNav collapsed={collapsed && !mobileNavOpen} onNavigate={onCloseMobileNav} />
  return (
    <Page
      variant="docked"
      isDockExpanded={mobileNavOpen}
      isDockTextExpanded={!collapsed}
      masthead={<OperatorHeader variant="mobile" expanded={mobileNavOpen} onToggle={onToggleMobileNav} />}
      dockContent={<OperatorHeader variant="docked" expanded={!collapsed} onToggle={onToggleDesktopNav} navigation={navigation} />}
      mainContainerId="swallow-main-content"
      skipToContent={<SkipToContent href="#swallow-main-content">Skip to content</SkipToContent>}
      className="sw-operator-shell"
    >
      <PageSection className="sw-page-section" isFilled><div className="sw-page-content">{children}</div></PageSection>
    </Page>
  )
}
