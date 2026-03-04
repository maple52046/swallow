import { Group, Burger, Text, Menu, ActionIcon, useMantineColorScheme, Tooltip, Avatar } from '@mantine/core'
import { useNavigate } from 'react-router-dom'
import { IconSun, IconMoon, IconSettings, IconLogout, IconServer } from '@tabler/icons-react'
import { t } from '@/presentation/app/i18n'
import { saveColorScheme } from '@/presentation/app/theme'

interface HeaderProps {
  opened: boolean
  toggle: () => void
}

export function Header({ opened, toggle }: HeaderProps) {
  const navigate = useNavigate()
  const { colorScheme, setColorScheme } = useMantineColorScheme()

  const isDark = colorScheme === 'dark'

  const toggleTheme = () => {
    const next = isDark ? 'light' : 'dark'
    setColorScheme(next)
    saveColorScheme(next)
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

        <Menu position="bottom-end" withArrow>
          <Menu.Target>
            <Avatar color="blue" radius="xl" size="sm" style={{ cursor: 'pointer' }}>
              OP
            </Avatar>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>ops@datacenter.io</Menu.Label>
            <Menu.Item leftSection={<IconSettings size={14} />} onClick={() => navigate('/settings')}>
              {t('nav.settings')}
            </Menu.Item>
            <Menu.Divider />
            <Menu.Item leftSection={<IconLogout size={14} />} color="red">
              {t('nav.signOut')}
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>
      </Group>
    </Group>
  )
}
