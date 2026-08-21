import { Avatar, Badge, Button, DropdownMenu, Flex, IconButton, Text, Tooltip } from '@radix-ui/themes'
import { useNavigate } from 'react-router-dom'
import {
  DesktopIcon,
  ExitIcon,
  HamburgerMenuIcon,
  MoonIcon,
  SunIcon,
} from '@radix-ui/react-icons'
import { t } from '@/presentation/app/i18n'
import { useAppearance } from '@/presentation/app/theme/appearanceContext'
import { useMediaQuery } from '@/presentation/components/radix/useMediaQuery'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface HeaderProps {
  /** Toggles the mobile nav overlay; the burger that calls it is shown only on mobile. */
  onToggleNav: () => void
}

/** Role-to-colour for the badge in the user menu. Text always names the role too. */
const ROLE_COLOR: Record<string, 'red' | 'blue' | 'gray'> = {
  admin: 'red',
  owner: 'blue',
  user: 'gray',
}

/**
 * The top application bar: brand, light/dark toggle, and the user menu.
 *
 * The colour-scheme toggle goes through `useAppearance` so the choice updates the live
 * Radix theme and persists. The burger appears only below the mobile breakpoint, matching
 * the shell's overlay behaviour. The user menu reads the current session from `useAuth`;
 * signing out clears it and returns to login.
 */
export function Header({ onToggleNav }: HeaderProps) {
  const navigate = useNavigate()
  const { appearance, toggle } = useAppearance()
  const { currentUser, logout } = useAuth()
  const isMobile = useMediaQuery('(max-width: 768px)')

  const isDark = appearance === 'dark'

  const initials =
    currentUser?.displayName
      .split(' ')
      .map((part) => part[0]?.toUpperCase())
      .join('')
      .slice(0, 2) ?? 'NA'

  const handleLogout = () => {
    logout()
    navigate('/login', { replace: true })
  }

  return (
    <Flex height="100%" px="4" align="center" justify="between">
      <Flex align="center" gap="3">
        {isMobile && (
          <IconButton variant="ghost" color="gray" aria-label="Toggle navigation" onClick={onToggleNav}>
            <HamburgerMenuIcon />
          </IconButton>
        )}
        <Flex align="center" gap="2" style={{ cursor: 'pointer' }} onClick={() => navigate('/')}>
          <DesktopIcon color="var(--accent-9)" />
          <Text weight="bold" size="4" color="blue">
            DC Dashboard
          </Text>
        </Flex>
      </Flex>

      <Flex align="center" gap="2">
        <Tooltip content={isDark ? t('settings.lightMode') : t('settings.darkMode')}>
          <IconButton
            variant="ghost"
            color="gray"
            aria-label={isDark ? t('settings.lightMode') : t('settings.darkMode')}
            onClick={toggle}
          >
            {isDark ? <SunIcon /> : <MoonIcon />}
          </IconButton>
        </Tooltip>

        {currentUser ? (
          <DropdownMenu.Root>
            <DropdownMenu.Trigger>
              <Button variant="ghost" color="gray" aria-label="Account menu">
                <Avatar size="1" radius="full" fallback={initials} color="blue" />
              </Button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Content align="end">
              <DropdownMenu.Label>
                <Flex direction="column" gap="1">
                  <Flex align="center" gap="2">
                    <Text size="1">{currentUser.displayName}</Text>
                    <Badge size="1" color={ROLE_COLOR[currentUser.role] ?? 'gray'}>
                      {currentUser.role}
                    </Badge>
                  </Flex>
                  <Text size="1" color="gray">
                    {currentUser.username}
                  </Text>
                </Flex>
              </DropdownMenu.Label>
              <DropdownMenu.Separator />
              <DropdownMenu.Item color="red" onSelect={handleLogout}>
                <ExitIcon />
                {t('nav.signOut')}
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Root>
        ) : (
          <Button variant="ghost" size="1" onClick={() => navigate('/login')}>
            Login
          </Button>
        )}
      </Flex>
    </Flex>
  )
}
