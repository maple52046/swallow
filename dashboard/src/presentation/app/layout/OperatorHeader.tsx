import { useState, type ReactNode } from 'react'
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownList,
  Masthead,
  MastheadBrand,
  MastheadContent,
  MastheadLogo,
  MastheadMain,
  MastheadToggle,
  MenuToggle,
  PageToggleButton,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
} from '@patternfly/react-core'
import {
  AdjustIcon,
  MapMarkerAltIcon,
  SignOutAltIcon,
  UserCircleIcon,
} from '@patternfly/react-icons'
import { useNavigate } from 'react-router-dom'
import { useAppearance } from '@/presentation/app/theme/appearanceContext'
import type { AppearanceMode } from '@/presentation/app/theme'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

interface OperatorHeaderProps {
  variant: 'mobile' | 'docked'
  expanded: boolean
  onToggle: () => void
  navigation?: ReactNode
}

/**
 * PatternFly masthead used in horizontal mobile and vertical docked forms.
 * Site, appearance, and account menus share the same URL/storage/auth boundaries in both
 * forms, so switching viewport cannot produce a second source of state.
 */
export function OperatorHeader({ variant, expanded, onToggle, navigation }: OperatorHeaderProps) {
  const navigate = useNavigate()
  const { mode, setMode } = useAppearance()
  const { currentUser, logout } = useAuth()
  const { sites, siteId, setSite, scopedHref } = useSiteScope()
  const [siteOpen, setSiteOpen] = useState(false)
  const [themeOpen, setThemeOpen] = useState(false)
  const [accountOpen, setAccountOpen] = useState(false)
  const isDocked = variant === 'docked'
  const selectedSite = sites.find((site) => site.id === siteId)?.name ?? 'All sites'

  const signOut = () => {
    logout()
    navigate('/login', { replace: true })
  }

  const brand = (
    <MastheadBrand className="sw-masthead-brand">
      <MastheadLogo
        component={(props) => <a {...props} href={scopedHref('/')} onClick={(event) => {
          event.preventDefault()
          navigate(scopedHref('/'))
        }} aria-label="Swallow home" />}
      >
        <span className="sw-brand-content"><SwallowLogo /><span className="sw-brand-name">Swallow</span></span>
      </MastheadLogo>
    </MastheadBrand>
  )

  const navigationToggle = (
    <MastheadToggle>
      {isDocked ? (
        <Button
          variant="plain"
          isHamburger
          isExpanded={expanded}
          onClick={onToggle}
          aria-label="Global navigation"
          aria-expanded={expanded}
        />
      ) : (
        <PageToggleButton
          variant="plain"
          aria-label="Global navigation"
          isSidebarOpen={expanded}
          onSidebarToggle={onToggle}
          isHamburgerButton
        />
      )}
    </MastheadToggle>
  )

  const utilities = (
    <ToolbarGroup
      variant="action-group-plain"
      align={{ default: 'alignEnd' }}
      gap={{ default: 'gapNone', md: 'gapSm' }}
    >
      <ToolbarItem>
        <Dropdown
          isOpen={siteOpen}
          onOpenChange={setSiteOpen}
          onSelect={(_event, value) => {
            setSite(value === '__all__' ? undefined : String(value))
            setSiteOpen(false)
          }}
          toggle={(toggleRef) => (
            <MenuToggle
              ref={toggleRef}
              variant="plain"
              icon={<MapMarkerAltIcon />}
              isDocked={isDocked}
              isTextExpanded={expanded}
              isExpanded={siteOpen}
              onClick={() => setSiteOpen((open) => !open)}
              aria-label={`Site scope: ${selectedSite}`}
            >
              {selectedSite}
            </MenuToggle>
          )}
        >
          <DropdownList>
            <DropdownItem value="__all__">All sites</DropdownItem>
            {sites.map((site) => <DropdownItem key={site.id} value={site.id}>{site.name}</DropdownItem>)}
          </DropdownList>
        </Dropdown>
      </ToolbarItem>
      <ToolbarItem>
        <Dropdown
          isOpen={themeOpen}
          onOpenChange={setThemeOpen}
          onSelect={(_event, value) => {
            setMode(String(value) as AppearanceMode)
            setThemeOpen(false)
          }}
          toggle={(toggleRef) => (
            <MenuToggle
              ref={toggleRef}
              variant="plain"
              icon={<AdjustIcon />}
              isDocked={isDocked}
              isTextExpanded={expanded}
              isExpanded={themeOpen}
              onClick={() => setThemeOpen((open) => !open)}
              aria-label={`Appearance: ${mode}`}
            >
              Appearance
            </MenuToggle>
          )}
        >
          <DropdownList>
            <DropdownItem value="system">System</DropdownItem>
            <DropdownItem value="light">Light</DropdownItem>
            <DropdownItem value="dark">Dark</DropdownItem>
          </DropdownList>
        </Dropdown>
      </ToolbarItem>
      {currentUser && (
        <ToolbarItem>
          <Dropdown
            isOpen={accountOpen}
            onOpenChange={setAccountOpen}
            toggle={(toggleRef) => (
              <MenuToggle
                ref={toggleRef}
                variant="plain"
                icon={<UserCircleIcon />}
                isDocked={isDocked}
                isTextExpanded={expanded}
                isExpanded={accountOpen}
                onClick={() => setAccountOpen((open) => !open)}
                aria-label="Account menu"
              >
                {currentUser.displayName}
              </MenuToggle>
            )}
          >
            <DropdownList>
              <DropdownItem isDisabled description={currentUser.role}>{currentUser.username}</DropdownItem>
              <DropdownItem icon={<SignOutAltIcon />} onClick={signOut}>Sign out</DropdownItem>
            </DropdownList>
          </Dropdown>
        </ToolbarItem>
      )}
    </ToolbarGroup>
  )

  return (
    <Masthead variant={isDocked ? 'docked' : 'default'} id={`swallow-${variant}-masthead`}>
      <MastheadMain className="sw-masthead-main">
        {brand}
        {navigationToggle}
      </MastheadMain>
      <MastheadContent>
        <Toolbar isVertical={isDocked} isStatic={!isDocked}>
          <ToolbarContent>
            {isDocked && <ToolbarItem>{navigation}</ToolbarItem>}
            {utilities}
          </ToolbarContent>
        </Toolbar>
      </MastheadContent>
    </Masthead>
  )
}
