import { useEffect, useState, useCallback } from 'react'
import {
  Table, Group, TextInput, Select, Text, Tabs,
  Stack, ActionIcon, ThemeIcon, Tooltip,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import {
  IconSearch, IconServer, IconCpu, IconDatabase, IconNetworkOff,
  IconSparkles, IconRefresh,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Host, StorageDevice, NetworkSwitch } from '@/domain/asset/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'

export function InventoryPage() {
  const { datacenter, missions } = useApp()
  const navigate = useNavigate()

  const [hosts, setHosts] = useState<Host[]>([])
  const [storage, setStorage] = useState<StorageDevice[]>([])
  const [switches, setSwitches] = useState<NetworkSwitch[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const [hostResult, storageResult, switchResult] = await Promise.all([
        datacenter.listHosts.execute({}),
        datacenter.listStorage.execute(),
        datacenter.listSwitches.execute(),
      ])
      setHosts(hostResult)
      setStorage(storageResult)
      setSwitches(switchResult)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [datacenter.listHosts, datacenter.listStorage, datacenter.listSwitches])

  useEffect(() => { void load() }, [load])

  const handleIPMIMission = async (host: Host) => {
    const mission = await missions.create.execute({
      name: `IPMI Sensors — ${host.name}`,
      goal: `Collect IPMI sensor data from host ${host.name} (BMC: ${host.bmcAddress}). Check temperature, power, fans.`,
      model: 'gpt-4o',
      target: host.id,
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 60 },
      permissions: { allowedPlugins: ['ipmi', 'ssh'], allowedTargets: [host.id], guardrails: ['read-only'] },
      tags: ['ipmi', 'inventory'],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'green' })
    navigate(`/missions/${mission.id}`)
  }

  const filteredHosts = hosts.filter((h) => {
    if (statusFilter && h.status !== statusFilter) return false
    if (search && !h.name.toLowerCase().includes(search.toLowerCase()) && !h.ipAddress?.includes(search)) return false
    return true
  })

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  const onlineCount = hosts.filter((h) => h.status === 'healthy').length

  return (
    <>
      <PageHeader
        title={t('asset.titlePlural')}
        subtitle={`${hosts.length} hosts · ${onlineCount} healthy`}
        actions={
          <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
        }
      />

      <Tabs defaultValue="hosts">
        <Tabs.List mb="md">
          <Tabs.Tab value="hosts" leftSection={<IconServer size={14} />}>
            {t('asset.host.titlePlural')} ({hosts.length})
          </Tabs.Tab>
          <Tabs.Tab value="storage" leftSection={<IconDatabase size={14} />}>
            {t('asset.storage.title')} ({storage.length})
          </Tabs.Tab>
          <Tabs.Tab value="switches" leftSection={<IconNetworkOff size={14} />}>
            {t('asset.switch.title')} ({switches.length})
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="hosts">
          <Group gap="sm" mb="md">
            <TextInput
              placeholder={t('common.search')}
              leftSection={<IconSearch size={14} />}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              w={240}
            />
            <Select
              placeholder="Status"
              data={[
                { value: 'healthy', label: t('asset.status.healthy') },
                { value: 'degraded', label: t('asset.status.degraded') },
                { value: 'critical', label: t('asset.status.critical') },
                { value: 'offline', label: t('asset.status.offline') },
              ]}
              value={statusFilter}
              onChange={setStatusFilter}
              clearable
              w={130}
            />
          </Group>

          {filteredHosts.length === 0 ? (
            <EmptyState message={t('asset.empty')} />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                  <Table.Th>{t('asset.host.cpuModel')}</Table.Th>
                  <Table.Th>{t('asset.host.memoryGB')}</Table.Th>
                  <Table.Th>{t('asset.host.gpuCount')}</Table.Th>
                  <Table.Th>{t('asset.host.ipAddress')}</Table.Th>
                  <Table.Th>{t('asset.host.os')}</Table.Th>
                  <Table.Th></Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {filteredHosts.map((host) => (
                  <Table.Tr key={host.id}>
                    <Table.Td>
                      <Group gap="xs">
                        <ThemeIcon size="sm" variant="light" color={host.status === 'healthy' ? 'green' : host.status === 'offline' ? 'gray' : 'red'}>
                          <IconServer size={12} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text size="sm" fw={500}>{host.name}</Text>
                          <Text size="xs" c="dimmed" ff="mono">{host.id}</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td><StatusBadge status={host.status} /></Table.Td>
                    <Table.Td><Text size="sm">{host.cpuModel}</Text></Table.Td>
                    <Table.Td><Text size="sm">{host.memoryGB} GB</Text></Table.Td>
                    <Table.Td>
                      <Group gap="xs">
                        <IconCpu size={12} />
                        <Text size="sm">{host.gpuCount}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td><Text size="sm" ff="mono">{host.ipAddress}</Text></Table.Td>
                    <Table.Td><Text size="sm">{host.os}</Text></Table.Td>
                    <Table.Td>
                      <Tooltip label="Create IPMI Mission">
                        <ActionIcon variant="subtle" size="sm" onClick={() => void handleIPMIMission(host)}>
                          <IconSparkles size={14} />
                        </ActionIcon>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="storage">
          {storage.length === 0 ? (
            <EmptyState message={t('asset.empty')} />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>{t('common.type')}</Table.Th>
                  <Table.Th>{t('asset.storage.capacity')}</Table.Th>
                  <Table.Th>{t('asset.storage.used')}</Table.Th>
                  <Table.Th>{t('asset.storage.latency')}</Table.Th>
                  <Table.Th>{t('asset.storage.iops')}</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {storage.map((s) => (
                  <Table.Tr key={s.id}>
                    <Table.Td><Text size="sm" fw={500}>{s.name}</Text></Table.Td>
                    <Table.Td><Text size="sm">{s.type}</Text></Table.Td>
                    <Table.Td><Text size="sm">{s.capacityTB.toFixed(1)} TB</Text></Table.Td>
                    <Table.Td><Text size="sm">{s.usedTB.toFixed(1)} TB ({Math.round(s.usedTB / s.capacityTB * 100)}%)</Text></Table.Td>
                    <Table.Td><Text size="sm">{s.latencyMs.toFixed(1)} ms</Text></Table.Td>
                    <Table.Td><Text size="sm">{s.iops.toLocaleString()}</Text></Table.Td>
                    <Table.Td><StatusBadge status={s.status} /></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="switches">
          {switches.length === 0 ? (
            <EmptyState message={t('asset.empty')} />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>Model</Table.Th>
                  <Table.Th>{t('asset.switch.ports')}</Table.Th>
                  <Table.Th>{t('asset.switch.activePorts')}</Table.Th>
                  <Table.Th>{t('asset.switch.speed')}</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {switches.map((sw) => (
                  <Table.Tr key={sw.id}>
                    <Table.Td><Text size="sm" fw={500}>{sw.name}</Text></Table.Td>
                    <Table.Td><Text size="sm">{sw.model}</Text></Table.Td>
                    <Table.Td><Text size="sm">{sw.portCount}</Text></Table.Td>
                    <Table.Td><Text size="sm">{sw.activePorts}</Text></Table.Td>
                    <Table.Td><Text size="sm">{sw.speed}</Text></Table.Td>
                    <Table.Td><StatusBadge status={sw.status} /></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
