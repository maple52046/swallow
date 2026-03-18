import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ActionIcon, Badge, Button, Checkbox, Group, Select, Stack,
  Table, Text, TextInput, Tooltip,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import {
  IconBuildingCommunity, IconRefresh, IconSearch,
  IconUserCheck, IconPower, IconRotate, IconTool, IconTerminal2,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { useAuth } from '@/presentation/contexts/AuthContext'
import type { Server } from '@/domain/server/types'
import type { Team } from '@/domain/team/types'
import { getAllocationState } from '@/domain/server/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { AllocationModal } from './AllocationModal'
import { formatRelative } from '@/shared/utils/time'

function ServerStatusBadge({ status }: { status: Server['status'] }) {
  const map: Record<Server['status'], { color: string; label: string }> = {
    live: { color: 'green', label: 'Live' },
    warning: { color: 'yellow', label: 'Warning' },
    error: { color: 'red', label: 'Error' },
    maintain: { color: 'blue', label: 'Maintain' },
    offline: { color: 'gray', label: 'Offline' },
  }
  const { color, label } = map[status]
  return <Badge color={color} variant="light" size="sm">{label}</Badge>
}

function GpuBadge({ gpuType, gpuCount }: { gpuType: string; gpuCount: number }) {
  if (gpuCount === 0 || !gpuType) return <Text size="sm" c="dimmed">—</Text>

  const upper = gpuType.toUpperCase()
  let vendor = ''
  let model = gpuType
  let color = 'gray'
  let variant: 'filled' | 'light' = 'light'

  if (upper.startsWith('NVIDIA')) {
    vendor = 'NVIDIA'; model = gpuType.slice('NVIDIA'.length).trim(); color = 'green'; variant = 'filled'
  } else if (upper.startsWith('AMD')) {
    vendor = 'AMD'; model = gpuType.slice('AMD'.length).trim(); color = 'dark'; variant = 'filled'
  }

  return (
    <Group gap={4} wrap="nowrap" align="center">
      {vendor ? <Badge color={color} variant={variant} size="sm" w={58} style={{ textAlign: 'center' }}>{vendor}</Badge> : null}
      <Text size="sm">{model}</Text>
      <Text size="xs" c="dimmed">×{gpuCount}</Text>
    </Group>
  )
}

function AllocationBadge({
  server, teams, onClick,
}: { server: Server; teams: Team[]; onClick: () => void }) {
  const state = getAllocationState(server)
  if (state === 'team') {
    const team = teams.find((t) => t.id === server.ownerTeamId)
    return (
      <Button variant="light" color="blue" size="xs" leftSection={<IconBuildingCommunity size={12} />} onClick={onClick}>
        {team?.name ?? server.ownerTeamId}
      </Button>
    )
  }
  if (state === 'user') {
    return (
      <Button variant="light" color="orange" size="xs" leftSection={<IconUserCheck size={12} />} onClick={onClick}>
        {server.ownerUserId}
      </Button>
    )
  }
  return (
    <Button variant="subtle" color="gray" size="xs" onClick={onClick}>
      Free
    </Button>
  )
}

export function ServersPage() {
  const { servers, teams } = useApp()
  const { currentUser } = useAuth()
  const [serverList, setServerList] = useState<Server[]>([])
  const [teamList, setTeamList] = useState<Team[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<string | null>(null)
  const [allocationSearch, setAllocationSearch] = useState('')
  const [selectedServer, setSelectedServer] = useState<Server | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [gpuVendorFilter, setGpuVendorFilter] = useState<string | null>(null)
  const [gpuSearch, setGpuSearch] = useState('')
  const isAdmin = currentUser?.role === 'admin'

  const gpuVendorOptions = useMemo(() => {
    const vendors = new Set<string>()
    serverList.forEach((s) => {
      if (!s.gpuType || s.gpuCount === 0) { vendors.add('none') }
      else if (s.gpuType.toUpperCase().startsWith('NVIDIA')) { vendors.add('NVIDIA') }
      else if (s.gpuType.toUpperCase().startsWith('AMD')) { vendors.add('AMD') }
      else { vendors.add('other') }
    })
    const map: Record<string, string> = { NVIDIA: 'NVIDIA', AMD: 'AMD', none: 'No GPU', other: 'Other' }
    return [...vendors].map((v) => ({ value: v, label: map[v] ?? v }))
  }, [serverList])

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const [srvResult, teamResult] = await Promise.all([
        servers.list.execute(),
        teams.list.execute(),
      ])
      setServerList(srvResult)
      setTeamList(teamResult)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [servers.list, teams.list])

  useEffect(() => { void load() }, [load])

  const handleAllocationClick = (server: Server) => {
    setSelectedServer(server)
    setModalOpen(true)
  }

  const handleAllocationUpdated = (updated: Server) => {
    setServerList((prev) => prev.map((s) => (s.id === updated.id ? updated : s)))
  }

  const handleStatusAction = async (server: Server, action: 'power_on' | 'power_off' | 'reboot' | 'maintain') => {
    try {
      let newStatus: Server['status']
      if (action === 'power_on') newStatus = 'live'
      else if (action === 'power_off') newStatus = 'offline'
      else if (action === 'reboot') newStatus = 'live'
      else newStatus = server.status === 'maintain' ? 'live' : 'maintain'

      const updated = await servers.updateStatus.execute(server.id, newStatus)
      setServerList((prev) => prev.map((s) => (s.id === updated.id ? updated : s)))
      notifications.show({ title: 'Status updated', message: `${server.hostname} → ${newStatus}`, color: 'green' })
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  const filtered = serverList.filter((s) => {
    if (statusFilter && s.status !== statusFilter) return false
    if (allocationSearch) {
      const q = allocationSearch.toLowerCase()
      const state = getAllocationState(s)
      if (state === 'free') {
        if (!'free'.includes(q)) return false
      } else if (state === 'team') {
        const team = teamList.find((t) => t.id === s.ownerTeamId)
        const label = (team?.name ?? s.ownerTeamId ?? '').toLowerCase()
        if (!label.includes(q)) return false
      } else if (state === 'user') {
        if (!(s.ownerUserId ?? '').toLowerCase().includes(q)) return false
      }
    }
    if (search) {
      const q = search.toLowerCase()
      if (!s.hostname.toLowerCase().includes(q) && !s.ip.includes(q)) return false
    }
    if (gpuVendorFilter) {
      if (gpuVendorFilter === 'none' && s.gpuCount > 0) return false
      if (gpuVendorFilter === 'NVIDIA' && !s.gpuType.toUpperCase().startsWith('NVIDIA')) return false
      if (gpuVendorFilter === 'AMD' && !s.gpuType.toUpperCase().startsWith('AMD')) return false
    }
    if (gpuSearch) {
      const q = gpuSearch.toLowerCase()
      if (!s.gpuType.toLowerCase().includes(q)) return false
    }
    return true
  })

  const freeCount = serverList.filter((s) => s.ownerTeamId === null && s.ownerUserId === null).length

  const allFilteredIds = filtered.map((s) => s.id)
  const allChecked = allFilteredIds.length > 0 && allFilteredIds.every((id) => selectedIds.has(id))
  const indeterminate = !allChecked && allFilteredIds.some((id) => selectedIds.has(id))

  const toggleAll = () => {
    if (allChecked) {
      setSelectedIds((prev) => {
        const next = new Set(prev)
        allFilteredIds.forEach((id) => next.delete(id))
        return next
      })
    } else {
      setSelectedIds((prev) => new Set([...prev, ...allFilteredIds]))
    }
  }

  const toggleOne = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title="Servers"
        subtitle={`${serverList.length} total · ${freeCount} free`}
        actions={
          <ActionIcon variant="default" onClick={() => void load()}>
            <IconRefresh size={16} />
          </ActionIcon>
        }
      />

      <Group gap="sm" mb="md">
        <TextInput
          placeholder="Search hostname / IP"
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          w={220}
        />
        <Select
          placeholder="Status"
          data={[
            { value: 'live', label: 'Live' },
            { value: 'warning', label: 'Warning' },
            { value: 'error', label: 'Error' },
            { value: 'maintain', label: 'Maintain' },
            { value: 'offline', label: 'Offline' },
          ]}
          value={statusFilter}
          onChange={setStatusFilter}
          clearable
          w={130}
          comboboxProps={{ withinPortal: false }}
        />
        <TextInput
          placeholder="Search allocation"
          leftSection={<IconSearch size={14} />}
          value={allocationSearch}
          onChange={(e) => setAllocationSearch(e.target.value)}
          w={180}
        />
        <Select
          placeholder="GPU Vendor"
          data={gpuVendorOptions}
          value={gpuVendorFilter}
          onChange={setGpuVendorFilter}
          clearable
          w={140}
          comboboxProps={{ withinPortal: false }}
        />
        <TextInput
          placeholder="GPU model"
          leftSection={<IconSearch size={14} />}
          value={gpuSearch}
          onChange={(e) => setGpuSearch(e.target.value)}
          w={160}
        />
      </Group>

      {filtered.length === 0 ? (
        <EmptyState message="No servers found." />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th w={28}>
                <Checkbox
                  checked={allChecked}
                  indeterminate={indeterminate}
                  onChange={toggleAll}
                  size="xs"
                />
              </Table.Th>
              <Table.Th>Hostname</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Allocation</Table.Th>
              <Table.Th>IP</Table.Th>
              <Table.Th>CPU</Table.Th>
              <Table.Th>RAM (GB)</Table.Th>
              <Table.Th>GPU</Table.Th>
              <Table.Th>Last Seen</Table.Th>
              <Table.Th>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {filtered.map((server) => (
              <Table.Tr key={server.id}>
                <Table.Td>
                  <Checkbox
                    checked={selectedIds.has(server.id)}
                    onChange={() => toggleOne(server.id)}
                    size="xs"
                  />
                </Table.Td>
                <Table.Td>
                  <Stack gap={0}>
                    <Text size="sm" fw={500}>{server.hostname}</Text>
                    <Text size="xs" c="dimmed" ff="mono">{server.id}</Text>
                  </Stack>
                </Table.Td>
                <Table.Td><ServerStatusBadge status={server.status} /></Table.Td>
                <Table.Td>
                  <AllocationBadge server={server} teams={teamList} onClick={() => handleAllocationClick(server)} />
                </Table.Td>
                <Table.Td><Text size="sm" ff="mono">{server.ip}</Text></Table.Td>
                <Table.Td><Text size="sm">{server.cpuCores}</Text></Table.Td>
                <Table.Td><Text size="sm">{server.ramGB}</Text></Table.Td>
                <Table.Td>
                  <GpuBadge gpuType={server.gpuType} gpuCount={server.gpuCount} />
                </Table.Td>
                <Table.Td><Text size="xs" c="dimmed">{formatRelative(server.lastSeenAt)}</Text></Table.Td>
                <Table.Td>
                  <Group gap={4} wrap="nowrap">
                    <Tooltip label={server.status === 'offline' ? 'Power On' : 'Power Off'}>
                      <ActionIcon
                        variant="subtle"
                        size="sm"
                        disabled={!isAdmin}
                        onClick={() => void handleStatusAction(server, server.status === 'offline' ? 'power_on' : 'power_off')}
                      >
                        <IconPower size={14} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Reboot">
                      <ActionIcon
                        variant="subtle"
                        size="sm"
                        disabled={!isAdmin || server.status === 'offline'}
                        onClick={() => void handleStatusAction(server, 'reboot')}
                      >
                        <IconRotate size={14} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label={server.status === 'maintain' ? 'Exit Maintenance' : 'Set Maintenance'}>
                      <ActionIcon
                        variant="subtle"
                        size="sm"
                        color={server.status === 'maintain' ? 'blue' : undefined}
                        disabled={!isAdmin}
                        onClick={() => void handleStatusAction(server, 'maintain')}
                      >
                        <IconTool size={14} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Connect">
                      <ActionIcon variant="subtle" size="sm" disabled={server.status === 'offline'}>
                        <IconTerminal2 size={14} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}

      <AllocationModal
        server={selectedServer}
        teams={teamList}
        opened={modalOpen}
        onClose={() => { setModalOpen(false); setSelectedServer(null) }}
        onUpdated={handleAllocationUpdated}
      />
    </>
  )
}
