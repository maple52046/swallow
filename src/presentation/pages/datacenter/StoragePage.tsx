import { useEffect, useState } from 'react'
import { Text, Table, Progress, Group, Stack } from '@mantine/core'
import { useApp } from '@/di/AppProvider'
import type { StorageDevice } from '@/domain/asset/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'

export function StoragePage() {
  const { datacenter } = useApp()
  const [storage, setStorage] = useState<StorageDevice[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    datacenter.listStorage.execute().then(setStorage).catch(() => null).finally(() => setLoading(false))
  }, [datacenter.listStorage])

  if (loading) return <LoadingState />

  const totalCapacityTB = storage.reduce((s, d) => s + d.capacityTB, 0)
  const totalUsedTB = storage.reduce((s, d) => s + d.usedTB, 0)

  return (
    <>
      <PageHeader
        title={t('storageDC.title')}
        subtitle={`${totalUsedTB.toFixed(1)} TB used of ${totalCapacityTB.toFixed(1)} TB total`}
      />

      {storage.length === 0 ? (
        <EmptyState message={t('storageDC.empty')} />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('common.name')}</Table.Th>
              <Table.Th>Vendor</Table.Th>
              <Table.Th>{t('common.type')}</Table.Th>
              <Table.Th>{t('asset.storage.capacity')}</Table.Th>
              <Table.Th>Usage</Table.Th>
              <Table.Th>{t('asset.storage.latency')}</Table.Th>
              <Table.Th>{t('asset.storage.iops')}</Table.Th>
              <Table.Th>{t('common.status')}</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {storage.map((dev) => {
              const usagePct = Math.round((dev.usedTB / dev.capacityTB) * 100)
              return (
                <Table.Tr key={dev.id}>
                  <Table.Td><Text size="sm" fw={500}>{dev.name}</Text></Table.Td>
                  <Table.Td><Text size="sm">{dev.vendor}</Text></Table.Td>
                  <Table.Td><Text size="sm">{dev.type}</Text></Table.Td>
                  <Table.Td><Text size="sm">{dev.capacityTB.toFixed(1)} TB</Text></Table.Td>
                  <Table.Td>
                    <Stack gap={2}>
                      <Group justify="space-between">
                        <Text size="xs">{dev.usedTB.toFixed(1)} TB</Text>
                        <Text size="xs" c="dimmed">{usagePct}%</Text>
                      </Group>
                      <Progress value={usagePct} size="xs" color={usagePct > 90 ? 'red' : usagePct > 75 ? 'yellow' : 'blue'} />
                    </Stack>
                  </Table.Td>
                  <Table.Td><Text size="sm">{dev.latencyMs.toFixed(1)} ms</Text></Table.Td>
                  <Table.Td><Text size="sm">{dev.iops.toLocaleString()}</Text></Table.Td>
                  <Table.Td><StatusBadge status={dev.status} /></Table.Td>
                </Table.Tr>
              )
            })}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
