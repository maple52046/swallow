import { useEffect, useState } from 'react'
import { Text, Table, Badge } from '@mantine/core'
import { useApp } from '@/di/AppProvider'
import type { NetworkSwitch } from '@/domain/asset/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'

export function NetworkingPage() {
  const { datacenter } = useApp()
  const [switches, setSwitches] = useState<NetworkSwitch[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    datacenter.listSwitches.execute().then(setSwitches).catch(() => null).finally(() => setLoading(false))
  }, [datacenter.listSwitches])

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader title={t('networking.title')} subtitle={`${switches.length} network devices`} />

      {switches.length === 0 ? (
        <EmptyState message={t('networking.empty')} />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('common.name')}</Table.Th>
              <Table.Th>Vendor</Table.Th>
              <Table.Th>Model</Table.Th>
              <Table.Th>Site</Table.Th>
              <Table.Th>{t('asset.switch.ports')}</Table.Th>
              <Table.Th>{t('asset.switch.activePorts')}</Table.Th>
              <Table.Th>{t('asset.switch.speed')}</Table.Th>
              <Table.Th>{t('asset.switch.firmware')}</Table.Th>
              <Table.Th>{t('common.status')}</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {switches.map((sw) => (
              <Table.Tr key={sw.id}>
                <Table.Td><Text size="sm" fw={500}>{sw.name}</Text></Table.Td>
                <Table.Td><Text size="sm">{sw.vendor}</Text></Table.Td>
                <Table.Td><Text size="sm">{sw.model}</Text></Table.Td>
                <Table.Td><Text size="sm">{sw.site}</Text></Table.Td>
                <Table.Td><Text size="sm">{sw.portCount}</Text></Table.Td>
                <Table.Td>
                  <Badge size="sm" color={sw.activePorts === sw.portCount ? 'green' : 'yellow'}>
                    {sw.activePorts}/{sw.portCount}
                  </Badge>
                </Table.Td>
                <Table.Td><Text size="sm">{sw.speed}</Text></Table.Td>
                <Table.Td><Text size="sm">{sw.firmware}</Text></Table.Td>
                <Table.Td><StatusBadge status={sw.status} /></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
