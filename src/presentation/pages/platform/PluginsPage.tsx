import { useEffect, useState } from 'react'
import {
  Table, Badge, Group, Text, Switch, ThemeIcon, Stack, Tooltip, TextInput,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconSearch, IconPuzzle } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Plugin } from '@/domain/platform/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'

export function PluginsPage() {
  const { platform } = useApp()
  const [plugins, setPlugins] = useState<Plugin[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')

  const load = () => {
    platform.listPlugins.execute().then(setPlugins).catch(() => null).finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleToggle = async (plugin: Plugin) => {
    await platform.enableDisablePlugin.execute(plugin.id, !plugin.enabled)
    notifications.show({
      title: plugin.enabled ? 'Plugin disabled' : 'Plugin enabled',
      message: plugin.name,
      color: plugin.enabled ? 'orange' : 'green',
    })
    load()
  }

  const filtered = plugins.filter((p) =>
    !search || p.name.toLowerCase().includes(search.toLowerCase()) || p.description.toLowerCase().includes(search.toLowerCase()),
  )

  if (loading) return <LoadingState />

  const enabledCount = plugins.filter((p) => p.enabled).length

  return (
    <>
      <PageHeader
        title={t('platform.plugin.titlePlural')}
        subtitle={`${plugins.length} plugins · ${enabledCount} enabled`}
      />

      <TextInput
        placeholder={t('common.search')}
        leftSection={<IconSearch size={14} />}
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        w={280}
        mb="md"
      />

      {filtered.length === 0 ? (
        <EmptyState message={t('platform.plugin.empty')} />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('common.name')}</Table.Th>
              <Table.Th>{t('platform.plugin.vendor')}</Table.Th>
              <Table.Th>{t('platform.plugin.category')}</Table.Th>
              <Table.Th>{t('common.version')}</Table.Th>
              <Table.Th>{t('platform.plugin.scopes')}</Table.Th>
              <Table.Th>{t('platform.plugin.actions')}</Table.Th>
              <Table.Th>{t('common.enabled')}</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {filtered.map((plugin) => (
              <Table.Tr key={plugin.id}>
                <Table.Td>
                  <Group gap="xs">
                    <ThemeIcon size="sm" variant="light" color="violet"><IconPuzzle size={12} /></ThemeIcon>
                    <Stack gap={0}>
                      <Text size="sm" fw={500}>{plugin.name}</Text>
                      <Text size="xs" c="dimmed">{plugin.description}</Text>
                    </Stack>
                  </Group>
                </Table.Td>
                <Table.Td><Text size="sm">{plugin.vendor ?? '—'}</Text></Table.Td>
                <Table.Td><Badge size="xs" variant="dot">{plugin.category}</Badge></Table.Td>
                <Table.Td><Badge size="xs" variant="outline">{plugin.version}</Badge></Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    {plugin.scopes.slice(0, 3).map((s) => <Badge key={s} size="xs" variant="outline" color="gray">{s}</Badge>)}
                    {plugin.scopes.length > 3 && <Text size="xs" c="dimmed">+{plugin.scopes.length - 3}</Text>}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    {plugin.actions.slice(0, 3).map((a) => (
                      <Badge key={a.name} size="xs" color="blue">{a.name}</Badge>
                    ))}
                    {plugin.actions.length > 3 && <Text size="xs" c="dimmed">+{plugin.actions.length - 3}</Text>}
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Tooltip label={plugin.enabled ? t('platform.plugin.disable') : t('platform.plugin.enable')}>
                    <Switch checked={plugin.enabled} onChange={() => void handleToggle(plugin)} size="sm" />
                  </Tooltip>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
