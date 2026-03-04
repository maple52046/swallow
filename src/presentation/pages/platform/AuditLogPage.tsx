import { useEffect, useState, useCallback } from 'react'
import {
  Table, Badge, Group, Text, TextInput, Select, ActionIcon, Stack, Code,
} from '@mantine/core'
import { IconSearch, IconRefresh } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { AuditEvent } from '@/domain/audit/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { formatDateTime, formatRelative } from '@/shared/utils/time'

function eventColor(type: string) {
  if (type.includes('created') || type.includes('started') || type.includes('enabled')) return 'green'
  if (type.includes('deleted') || type.includes('canceled') || type.includes('failed') || type.includes('disabled')) return 'red'
  if (type.includes('updated') || type.includes('upgraded')) return 'blue'
  if (type.includes('paused') || type.includes('archived')) return 'orange'
  return 'gray'
}

export function AuditLogPage() {
  const { platform } = useApp()
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [resourceFilter, setResourceFilter] = useState<string | null>(null)

  const load = useCallback(() => {
    platform.listAuditEvents.execute({ limit: 200 }).then(setEvents).catch(() => null).finally(() => setLoading(false))
  }, [platform.listAuditEvents])

  useEffect(() => { load() }, [load])

  const filtered = events.filter((e) => {
    if (resourceFilter && e.resourceType !== resourceFilter) return false
    if (search && !e.type.toLowerCase().includes(search.toLowerCase()) &&
        !e.actor.toLowerCase().includes(search.toLowerCase()) &&
        !e.resourceId.toLowerCase().includes(search.toLowerCase()) &&
        !e.description.toLowerCase().includes(search.toLowerCase())) return false
    return true
  })

  const resourceTypes = [...new Set(events.map((e) => e.resourceType))]

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader
        title={t('platform.audit.title')}
        subtitle={`${events.length} events`}
        actions={<ActionIcon variant="default" onClick={load}><IconRefresh size={16} /></ActionIcon>}
      />

      <Group gap="sm" mb="md">
        <TextInput
          placeholder={t('common.search')}
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          w={240}
        />
        <Select
          placeholder="Resource type"
          data={resourceTypes.map((r) => ({ value: r, label: r }))}
          value={resourceFilter}
          onChange={setResourceFilter}
          clearable
          w={160}
        />
      </Group>

      {filtered.length === 0 ? (
        <EmptyState message={t('platform.audit.empty')} />
      ) : (
        <Table highlightOnHover fz="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('platform.audit.timestamp')}</Table.Th>
              <Table.Th>{t('platform.audit.event')}</Table.Th>
              <Table.Th>{t('platform.audit.actor')}</Table.Th>
              <Table.Th>{t('platform.audit.resource')}</Table.Th>
              <Table.Th>{t('platform.audit.resourceId')}</Table.Th>
              <Table.Th>Description</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {filtered.map((event) => (
              <Table.Tr key={event.id}>
                <Table.Td>
                  <Stack gap={0}>
                    <Text size="xs">{formatDateTime(event.timestamp)}</Text>
                    <Text size="xs" c="dimmed">{formatRelative(event.timestamp)}</Text>
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Badge size="sm" color={eventColor(event.type)}>{event.type}</Badge>
                </Table.Td>
                <Table.Td><Text size="sm">{event.actor}</Text></Table.Td>
                <Table.Td><Badge size="xs" variant="outline">{event.resourceType}</Badge></Table.Td>
                <Table.Td><Code fz="xs">{event.resourceId.slice(0, 12)}</Code></Table.Td>
                <Table.Td>
                  <Text size="xs" c="dimmed" lineClamp={1} maw={250}>{event.description}</Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
