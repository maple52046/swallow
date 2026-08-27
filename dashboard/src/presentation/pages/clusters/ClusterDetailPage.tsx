import { useCallback, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Box, Button, Callout, Card, Flex, Grid, Heading, Table, Text } from '@radix-ui/themes'
import { ExclamationTriangleIcon, ReloadIcon, UpdateIcon } from '@radix-ui/react-icons'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/radix/toast/toastContext'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { useClusterDetail } from './useClusterDetail'

/**
 * One cluster: its members (controllers and workers), membership freshness, and the
 * operations that concern it.
 *
 * Members come from the servers carrying this cluster's membership axis, so a dedicated k0s
 * controller — which is not a Kubernetes node — appears here through its control-plane lease
 * exactly as a worker does through the node list. "Sync now" reads membership immediately
 * instead of waiting for the background interval.
 */
export function ClusterDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { clusters } = useApp()
  const { showToast } = useToast()
  const state = useClusterDetail(id)
  const [syncing, setSyncing] = useState(false)

  const onSync = useCallback(async () => {
    if (!id) return
    setSyncing(true)
    try {
      const report = await clusters.syncCluster(id)
      showToast({
        tone: report.error ? 'error' : 'success',
        title: report.error ? 'Sync failed' : 'Membership synced',
        description: report.error
          ? report.error
          : `${report.matched} of ${report.members} members matched to servers.`,
      })
      if (state.status === 'ready') state.data.reload()
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Could not sync membership.'
      showToast({ tone: 'error', title: 'Sync failed', description: message })
    } finally {
      setSyncing(false)
    }
  }, [id, clusters, showToast, state])

  if (state.status === 'loading') {
    return (
      <Box>
        <PageHeader title="Cluster" />
        <LoadingState rows={6} />
      </Box>
    )
  }
  if (state.status === 'not-found') {
    return (
      <Box>
        <PageHeader title="Cluster" />
        <EmptyState title="Cluster not found" message="This cluster no longer exists." />
      </Box>
    )
  }
  if (state.status === 'error') {
    return (
      <Box>
        <PageHeader title="Cluster" />
        <ErrorState message={state.message} />
      </Box>
    )
  }

  const { cluster, members, operations, reload } = state.data
  const controllers = members.filter((server) => server.membership?.role === 'control-plane')
  const workers = members.filter((server) => server.membership?.role !== 'control-plane')

  return (
    <Box>
      <PageHeader
        title={cluster.name}
        subtitle={`${cluster.type} · GPU stack owned by ${cluster.gpuStackOwner} · exporters: ${cluster.exporterOwner}`}
        actions={
          <Button onClick={onSync} loading={syncing} disabled={syncing || !cluster.integrationId}>
            <UpdateIcon />
            Sync now
          </Button>
        }
      />

      {!cluster.integrationId && (
        <Callout.Root color="blue" mb="4" role="status">
          <Callout.Icon>
            <ReloadIcon />
          </Callout.Icon>
          <Callout.Text>
            This cluster is not reachable yet. It becomes reachable once its deployment
            finishes and swallow records a read credential.
          </Callout.Text>
        </Callout.Root>
      )}

      {cluster.sync.lastError && (
        <Callout.Root color="red" mb="4" role="alert">
          <Callout.Icon>
            <ExclamationTriangleIcon />
          </Callout.Icon>
          <Callout.Text>
            Membership sync is failing: {cluster.sync.lastError}. The member list below may be
            stale (last succeeded {formatRelative(cluster.sync.lastSucceededAt ?? undefined)}).
          </Callout.Text>
        </Callout.Root>
      )}

      <Grid columns={{ initial: '1', md: '3' }} gap="3" mb="4">
        <Card>
          <Text size="1" color="gray">
            Control-plane
          </Text>
          <Heading size="6">{controllers.length}</Heading>
        </Card>
        <Card>
          <Text size="1" color="gray">
            Workers
          </Text>
          <Heading size="6">{workers.length}</Heading>
        </Card>
        <Card>
          <Text size="1" color="gray">
            Last synced
          </Text>
          <Heading size="6">
            {cluster.sync.lastSucceededAt ? formatRelative(cluster.sync.lastSucceededAt) : '—'}
          </Heading>
        </Card>
      </Grid>

      <Card mb="4">
        <Heading size="3" mb="2">
          Members
        </Heading>
        <MemberTable members={members} onSelect={(server) => navigate(`/servers/${server.id}`)} />
      </Card>

      <Card>
        <Flex justify="between" align="center" mb="2">
          <Heading size="3">Operations</Heading>
          <Button size="1" variant="ghost" onClick={reload} aria-label="Refresh">
            <ReloadIcon />
            Refresh
          </Button>
        </Flex>
        {operations.length === 0 ? (
          <EmptyState title="No operations" message="No automation has run against this cluster." />
        ) : (
          <Table.Root variant="ghost">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Intent</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Status</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Requested</Table.ColumnHeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {operations.map((operation) => (
                <Table.Row
                  key={operation.id}
                  onClick={() => navigate(`/operations/${operation.id}`)}
                  style={{ cursor: 'pointer' }}
                >
                  <Table.Cell>
                    <Text size="2">{operation.intent || operation.execution.playbook}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <StatusBadge status={operation.execution.status} />
                  </Table.Cell>
                  <Table.Cell>
                    <Text size="2" color="gray">
                      {formatDateTime(operation.requestedAt)}
                    </Text>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        )}
      </Card>
    </Box>
  )
}

/**
 * The cluster's members. Role and readiness come from each server's membership axis, which
 * the cluster's own API populated; a lease-discovered controller has no address, shown as a
 * dash rather than a blank cell.
 */
function MemberTable({
  members,
  onSelect,
}: {
  members: Server[]
  onSelect: (server: Server) => void
}) {
  if (members.length === 0) {
    return (
      <EmptyState
        title="No members yet"
        message="No server currently reports membership in this cluster."
      />
    )
  }

  return (
    <Table.Root variant="ghost">
      <Table.Header>
        <Table.Row>
          <Table.ColumnHeaderCell>Node</Table.ColumnHeaderCell>
          <Table.ColumnHeaderCell>Role</Table.ColumnHeaderCell>
          <Table.ColumnHeaderCell>State</Table.ColumnHeaderCell>
          <Table.ColumnHeaderCell>Address</Table.ColumnHeaderCell>
        </Table.Row>
      </Table.Header>
      <Table.Body>
        {members.map((server) => (
          <Table.Row
            key={server.id}
            onClick={() => onSelect(server)}
            style={{ cursor: 'pointer' }}
          >
            <Table.Cell>
              <Text size="2" weight="medium">
                {server.membership?.nodeName || serverDisplayName(server)}
              </Text>
            </Table.Cell>
            <Table.Cell>
              <StatusBadge
                status={server.membership?.role === 'control-plane' ? 'info' : 'neutral'}
                label={server.membership?.role || 'unknown'}
              />
            </Table.Cell>
            <Table.Cell>
              <StatusBadge
                status={server.membership?.state === 'ready' ? 'succeeded' : 'warning'}
                label={server.membership?.state || 'unknown'}
              />
            </Table.Cell>
            <Table.Cell>
              <Text size="1" style={{ fontFamily: 'monospace' }}>
                {serverPrimaryAddress(server) ?? '—'}
              </Text>
            </Table.Cell>
          </Table.Row>
        ))}
      </Table.Body>
    </Table.Root>
  )
}
