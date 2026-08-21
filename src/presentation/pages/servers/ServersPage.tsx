import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Alert,
  Badge,
  Card,
  Group,
  Pagination,
  Select,
  Switch,
  Table,
  Text,
  TextInput,
} from '@mantine/core'
import { IconAlertCircle, IconSearch } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import {
  HealthBadge,
  MembershipBadge,
  ProvisioningBadge,
} from '@/presentation/components/AxisBadge'
import type { Server } from '@/domain/server/types'
import { serverDisplayName, serverPrimaryAddress } from '@/domain/server/types'
import type { Integration } from '@/domain/site/types'

const PAGE_SIZE = 20

type ListState =
  | { status: 'loading' }
  | { status: 'ready'; items: Server[]; total: number }
  | { status: 'error'; message: string }

const PROVISIONING_STATES = [
  'new',
  'commissioning',
  'ready',
  'allocated',
  'deploying',
  'deployed',
  'releasing',
  'testing',
  'rescue',
  'broken',
  'failed',
  'retired',
]

export function ServersPage() {
  const { servers, sites } = useApp()
  const navigate = useNavigate()

  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [state, setState] = useState<string | null>(null)
  const [includeAbsent, setIncludeAbsent] = useState(false)

  // One piece of state rather than parallel loading and error flags, and it is only
  // written from the fetch callbacks. A refetch keeps the previous rows on screen
  // instead of blanking the table on every keystroke.
  const [result, setResult] = useState<ListState>({ status: 'loading' })

  const [provisioners, setProvisioners] = useState<Integration[]>([])

  useEffect(() => {
    let cancelled = false

    servers
      .listServers({
        page,
        pageSize: PAGE_SIZE,
        keyword: keyword || undefined,
        provisioningState: state ?? undefined,
        includeAbsent,
      })
      .then((page) => {
        if (!cancelled) setResult({ status: 'ready', items: page.items, total: page.total })
      })
      .catch((err: Error) => {
        if (!cancelled) setResult({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [servers, page, keyword, state, includeAbsent])

  // Provisioner freshness, so that an empty or stale list has a visible reason.
  useEffect(() => {
    let cancelled = false
    sites
      .listIntegrations({ kind: 'provisioner' })
      .then((result) => {
        if (!cancelled) setProvisioners(result)
      })
      .catch(() => {
        // Non-essential context; the server list stands on its own.
      })
    return () => {
      cancelled = true
    }
  }, [sites])

  const staleProvisioners = useMemo(
    () => provisioners.filter((integration) => integration.sync.lastError !== null),
    [provisioners],
  )

  const items = result.status === 'ready' ? result.items : []
  const total = result.status === 'ready' ? result.total : 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <>
      <PageHeader
        title="Servers"
        subtitle="Projected from each site's provisioner. Servers are not created here."
      />

      {provisioners.length === 0 && result.status !== 'loading' && (
        <Alert
          icon={<IconAlertCircle size={16} />}
          color="yellow"
          mb="md"
          title="No provisioner registered"
        >
          Servers appear once a provisioner integration is registered and reconciled.
          Register one through the API; the dashboard does not handle credentials.
        </Alert>
      )}

      {staleProvisioners.map((integration) => (
        <Alert
          key={integration.id}
          icon={<IconAlertCircle size={16} />}
          color="orange"
          mb="md"
          title={`${integration.name} last sync failed`}
        >
          <Text size="sm">{integration.sync.lastError}</Text>
          <Text size="xs" c="dimmed" mt={4}>
            {integration.sync.lastSucceededAt
              ? `Showing data from ${new Date(integration.sync.lastSucceededAt).toLocaleString()}.`
              : 'This provisioner has never synced successfully.'}
          </Text>
        </Alert>
      ))}

      <Card withBorder mb="md">
        <Group>
          <TextInput
            placeholder="hostname, FQDN, or address"
            leftSection={<IconSearch size={14} />}
            value={keyword}
            onChange={(event) => {
              setKeyword(event.currentTarget.value)
              setPage(1)
            }}
            style={{ flex: 1 }}
          />
          <Select
            placeholder="Provisioning state"
            data={PROVISIONING_STATES}
            value={state}
            onChange={(value) => {
              setState(value)
              setPage(1)
            }}
            clearable
            w={200}
          />
          <Switch
            label="Include absent"
            checked={includeAbsent}
            onChange={(event) => {
              setIncludeAbsent(event.currentTarget.checked)
              setPage(1)
            }}
          />
        </Group>
      </Card>

      {result.status === 'loading' && <LoadingState />}
      {result.status === 'error' && <ErrorState message={result.message} />}

      {result.status === 'ready' && items.length === 0 && (
        <EmptyState title="No servers" message="Nothing matches these filters." />
      )}

      {result.status === 'ready' && items.length > 0 && (
        <Card withBorder p={0}>
          <Table highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Address</Table.Th>
                <Table.Th>Provisioning</Table.Th>
                <Table.Th>Cluster</Table.Th>
                <Table.Th>Health</Table.Th>
                <Table.Th>GPUs</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {items.map((server) => (
                <Table.Tr
                  key={server.id}
                  onClick={() => navigate(`/servers/${server.id}`)}
                  style={{ cursor: 'pointer' }}
                >
                  <Table.Td>
                    <Group gap="xs">
                      <Text size="sm">{serverDisplayName(server)}</Text>
                      {server.absent && (
                        <Badge color="gray" variant="outline" size="xs">
                          absent
                        </Badge>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" c={serverPrimaryAddress(server) ? undefined : 'dimmed'}>
                      {serverPrimaryAddress(server) ?? 'not assigned'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <ProvisioningBadge axis={server.provisioning} />
                  </Table.Td>
                  <Table.Td>
                    <MembershipBadge axis={server.membership} />
                  </Table.Td>
                  <Table.Td>
                    <HealthBadge axis={server.health} />
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">
                      {server.gpus.length === 0
                        ? '—'
                        : server.gpus
                            .map((gpu) => `${gpu.count}× ${gpu.model || gpu.vendor}`)
                            .join(', ')}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Card>
      )}

      {totalPages > 1 && (
        <Group justify="center" mt="md">
          <Pagination total={totalPages} value={page} onChange={setPage} />
        </Group>
      )}
    </>
  )
}
