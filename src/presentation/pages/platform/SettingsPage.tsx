import {
  Card, Stack, Text, Group, SegmentedControl, Badge, Table,
} from '@mantine/core'
import { useMantineColorScheme } from '@mantine/core'
import { IconSun, IconMoon } from '@tabler/icons-react'
import { saveColorScheme } from '@/presentation/app/theme'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'

export function SettingsPage() {
  const { colorScheme, setColorScheme } = useMantineColorScheme()

  const handleSchemeChange = (value: string) => {
    const cs = value as 'light' | 'dark'
    setColorScheme(cs)
    saveColorScheme(cs)
  }

  return (
    <>
      <PageHeader title={t('settings.title')} subtitle="Configure dashboard preferences" />

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
