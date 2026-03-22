import { Group, Burger, Text, Menu, ActionIcon, useMantineColorScheme, Tooltip, Avatar, Badge, Button } from '@mantine/core'
import { useNavigate } from 'react-router-dom'
import { IconSun, IconMoon, IconSettings, IconLogout, IconServer } from '@tabler/icons-react'
import { t } from '@/presentation/app/i18n'
import { saveColorScheme } from '@/presentation/app/theme'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface HeaderProps {
  opened: boolean
  toggle: () => void
}

export function Header({ opened, toggle }: HeaderProps) {
  const navigate = useNavigate()
  const { colorScheme, setColorScheme } = useMantineColorScheme()
  const { currentUser, logout } = useAuth()

  const isDark = colorScheme === 'dark'

  const toggleTheme = () => {
    const next = isDark ? 'light' : 'dark'
    setColorScheme(next)
    saveColorScheme(next)
  }

  const initials = currentUser?.displayName
    .split(' ')
    .map((p) => p[0]?.toUpperCase())
    .join('')
    .slice(0, 2) ?? 'NA'

  const handleLogout = () => {
    logout()
    navigate('/login', { replace: true })
  }

  return (
    <Group h="100%" px="md" justify="space-between">
      <Group gap="sm">
        <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
        <Group
          gap="xs"
          style={{ cursor: 'pointer' }}
          onClick={() => navigate('/')}
        >
          <IconServer size={22} color="var(--mantine-color-blue-5)" />
          <Text fw={700} size="lg" c="blue">
            DC Dashboard
          </Text>
        </Group>
      </Group>

      <Group gap="xs">
        <Tooltip label={isDark ? t('settings.lightMode') : t('settings.darkMode')}>
          <ActionIcon variant="subtle" onClick={toggleTheme} size="lg">
            {isDark ? <IconSun size={18} /> : <IconMoon size={18} />}
          </ActionIcon>
        </Tooltip>

        {currentUser ? (
          <Menu position="bottom-end" withArrow>
            <Menu.Target>
              <Avatar color="blue" radius="xl" size="sm" style={{ cursor: 'pointer' }}>
                {initials}
              </Avatar>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>
                <Group gap={6}>
                  <Text size="xs">{currentUser.displayName}</Text>
                  <Badge size="xs" variant="light" color={currentUser.role === 'admin' ? 'red' : currentUser.role === 'owner' ? 'blue' : 'gray'}>
                    {currentUser.role}
                  </Badge>
                </Group>
                <Text size="xs" c="dimmed">{currentUser.username}</Text>
              </Menu.Label>
              <Menu.Item leftSection={<IconSettings size={14} />} onClick={() => navigate('/account/settings')}>
                {t('nav.settings')}
              </Menu.Item>
              <Menu.Divider />
              <Menu.Item leftSection={<IconLogout size={14} />} color="red" onClick={handleLogout}>
                {t('nav.signOut')}
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        ) : (
          <Button variant="subtle" size="xs" onClick={() => navigate('/login')}>
            Login
          </Button>
        )}
      </Group>
    </Group>
  )
}
