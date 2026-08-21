import { useEffect, useState } from 'react'
import { Alert, Card, Group, SimpleGrid, Stack, Table, Text, Title } from '@mantine/core'
import { IconAlertCircle } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import type { Server } from '@/domain/server/types'
import type { Integration, Site } from '@/domain/site/types'

/**
 * Counts derived from one real listing, and integration freshness.
 *
 * The previous overview synthesised CPU, RAM, and GPU trend lines in the browser.
 * Nothing here is invented: if a number cannot be read from the API it is not shown.
 */
function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <Card withBorder>
      <Text size="xs" c="dimmed">
        {label}
      </Text>
      <Text fz={28} fw={600}>
        {value}
      </Text>
      {hint && (
        <Text size="xs" c="dimmed">
          {hint}
        </Text>
      )}
    </Card>
  )
}

type OverviewState =
  | { status: 'loading' }
  | { status: 'ready'; servers: Server[]; sites: Site[]; integrations: Integration[] }
  | { status: 'error'; message: string }

function freshness(integration: Integration): string {
  if (integration.sync.lastError) {
    return integration.sync.lastSucceededAt
      ? `failing since ${new Date(integration.sync.lastSucceededAt).toLocaleString()}`
      : 'never synced'
  }
  if (integration.sync.lastSucceededAt) {
    return `synced ${new Date(integration.sync.lastSucceededAt).toLocaleString()}`
  }
  return 'not synced yet'
}

export function OverviewPage() {
  const { servers, sites } = useApp()

  // Written only from the fetch callbacks, so nothing sets state synchronously during
  // the effect.
  const [state, setState] = useState<OverviewState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    Promise.all([
      // pageSize is the API maximum; the counts below are of what is returned, which is
      // stated on screen when it matters.
      servers.listServers({ pageSize: 100, includeAbsent: true }),
      sites.listSites(),
      sites.listIntegrations(),
    ])
      .then(([serverPage, siteResult, integrationResult]) => {
        if (cancelled) return
        setState({
          status: 'ready',
          servers: serverPage.items,
          sites: siteResult,
          integrations: integrationResult,
        })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [servers, sites])

  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} />

  const { servers: allServers, sites: siteList, integrations } = state
  const present = allServers.filter((server) => !server.absent)
  const deployed = present.filter((server) => server.provisioning?.state === 'deployed')
  const inCluster = present.filter((server) => server.membership !== null)
  const gpuServers = present.filter((server) => server.gpus.length > 0)
  const gpuCount = gpuServers.reduce(
    (sum, server) => sum + server.gpus.reduce((inner, gpu) => inner + gpu.count, 0),
    0,
  )
  const failing = integrations.filter((integration) => integration.sync.lastError !== null)

  return (
    <>
      <PageHeader title="Overview" subtitle="What the platform currently knows." />

      {integrations.length === 0 && (
        <Alert
          icon={<IconAlertCircle size={16} />}
          color="yellow"
          mb="md"
          title="Nothing is integrated yet"
        >
          gdcm reads everything from external systems. Register a site and at least one
          provisioner through the API to see servers here.
        </Alert>
      )}

      {failing.map((integration) => (
        <Alert
          key={integration.id}
          icon={<IconAlertCircle size={16} />}
          color="orange"
          mb="md"
          title={`${integration.name} is not answering`}
        >
          <Text size="sm">{integration.sync.lastError}</Text>
        </Alert>
      ))}

      <SimpleGrid cols={{ base: 2, md: 5 }} mb="lg">
        <Stat label="Sites" value={String(siteList.length)} />
        <Stat
          label="Servers"
          value={String(present.length)}
          hint={
            allServers.length - present.length > 0
              ? `${allServers.length - present.length} absent`
              : undefined
          }
        />
        <Stat label="Deployed" value={String(deployed.length)} />
        <Stat label="In a cluster" value={String(inCluster.length)} />
        <Stat
          label="GPUs"
          value={String(gpuCount)}
          hint={gpuServers.length > 0 ? `across ${gpuServers.length} servers` : undefined}
        />
      </SimpleGrid>

      {allServers.length >= 100 && (
        <Text size="xs" c="dimmed" mb="md">
          Counts cover the first 100 servers. Use the servers page for the full fleet.
        </Text>
      )}

      <Card withBorder>
        <Group justify="space-between" mb="sm">
          <Title order={5}>Integrations</Title>
          <Text size="xs" c="dimmed">
            Everything gdcm reads comes from one of these
          </Text>
        </Group>

        {integrations.length === 0 ? (
          <Text size="sm" c="dimmed">
            None registered.
          </Text>
        ) : (
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Kind</Table.Th>
                <Table.Th>Product</Table.Th>
                <Table.Th>Enabled</Table.Th>
                <Table.Th>Freshness</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {integrations.map((integration) => (
                <Table.Tr key={integration.id}>
                  <Table.Td>{integration.name}</Table.Td>
                  <Table.Td>{integration.kind}</Table.Td>
                  <Table.Td>{integration.providerKind}</Table.Td>
                  <Table.Td>{integration.enabled ? 'yes' : 'paused'}</Table.Td>
                  <Table.Td>
                    <Stack gap={0}>
                      <Text size="sm" c={integration.sync.lastError ? 'orange' : undefined}>
                        {freshness(integration)}
                      </Text>
                    </Stack>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Card>
    </>
  )
}
