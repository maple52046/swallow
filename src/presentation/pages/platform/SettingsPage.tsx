import { useEffect, useState } from 'react'
import {
  Card, Stack, Text, Group, SegmentedControl, Badge, Table,
  ThemeIcon,
} from '@mantine/core'
import { useMantineColorScheme } from '@mantine/core'
import { IconKey, IconMoon, IconSun } from '@tabler/icons-react'
import { saveColorScheme } from '@/presentation/app/theme'
import { t } from '@/presentation/app/i18n'
import { useApp } from '@/di/AppProvider'
import type { SSHKey } from '@/domain/asset/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { formatRelative } from '@/shared/utils/time'

export function SettingsPage() {
  const { colorScheme, setColorScheme } = useMantineColorScheme()
  const { datacenter } = useApp()
  const [sshKeys, setSshKeys] = useState<SSHKey[]>([])

  useEffect(() => {
    datacenter.listSSHKeys.execute()
      .then(setSshKeys)
      .catch(() => null)
  }, [datacenter.listSSHKeys])

  const handleSchemeChange = (value: string) => {
    const cs = value as 'light' | 'dark'
    setColorScheme(cs)
    saveColorScheme(cs)
  }

  return (
    <>
      <PageHeader
        title={t('settings.title')}
        subtitle="Configure dashboard preferences and manage your credentials"
      />

      <Stack gap="xl" maw={600}>
        <Card withBorder>
          <Text fw={500} mb="md">{t('settings.appearance')}</Text>
          <Stack gap="md">
            <Group justify="space-between">
              <Stack gap={0}>
                <Text size="sm">{t('settings.theme')}</Text>
                <Text size="xs" c="dimmed">Choose between light and dark mode</Text>
              </Stack>
              <SegmentedControl
                value={colorScheme}
                onChange={handleSchemeChange}
                data={[
                  { label: <Group gap={4}><IconSun size={14} />{t('settings.lightMode')}</Group>, value: 'light' },
                  { label: <Group gap={4}><IconMoon size={14} />{t('settings.darkMode')}</Group>, value: 'dark' },
                ]}
              />
            </Group>
          </Stack>
        </Card>

        <Card withBorder>
          <Text fw={500} mb="md">{t('settings.language')}</Text>
          <Group justify="space-between">
            <Stack gap={0}>
              <Text size="sm">English</Text>
              <Text size="xs" c="dimmed">{t('settings.languageNote')}</Text>
            </Stack>
            <Badge>EN</Badge>
          </Group>
        </Card>

        <Card withBorder>
          <Group gap="xs" mb="sm">
            <IconKey size={16} />
            <Text fw={500}>SSH Keys</Text>
          </Group>
          <Text size="xs" c="dimmed" mb="md">Manage your SSH public keys</Text>
          {sshKeys.length === 0 ? (
            <Text size="sm" c="dimmed">{t('access.empty.sshKeys')}</Text>
          ) : (
            <Stack gap="sm">
              {sshKeys.map((key) => (
                <Card key={key.id} withBorder p="sm">
                  <Group justify="space-between">
                    <Group gap="sm">
                      <ThemeIcon variant="light" size="sm"><IconKey size={14} /></ThemeIcon>
                      <Stack gap={0}>
                        <Text size="sm" fw={500}>{key.name}</Text>
                        <Text size="xs" c="dimmed" ff="mono">{key.fingerprint}</Text>
                      </Stack>
                    </Group>
                    <Group gap="xs">
                      <Text size="xs" c="dimmed">{key.vaultRef}</Text>
                      {key.createdAt && (
                        <Text size="xs" c="dimmed">{formatRelative(key.createdAt)}</Text>
                      )}
                    </Group>
                  </Group>
                </Card>
              ))}
            </Stack>
          )}
        </Card>

        <Card withBorder>
          <Text fw={500} mb="md">About</Text>
          <Table fz="sm" withRowBorders={false}>
            <Table.Tbody>
              <Table.Tr>
                <Table.Td c="dimmed">Application</Table.Td>
                <Table.Td>DC Dashboard</Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td c="dimmed">Version</Table.Td>
                <Table.Td><Badge variant="outline">0.1.0-alpha</Badge></Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td c="dimmed">Build</Table.Td>
                <Table.Td>Vite + React 19 + TypeScript 5.9</Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td c="dimmed">UI Framework</Table.Td>
                <Table.Td>Mantine 7</Table.Td>
              </Table.Tr>
            </Table.Tbody>
          </Table>
        </Card>
      </Stack>
    </>
  )
}
