import { useEffect, useState } from 'react'
import { Callout, Card, Flex, Grid, Heading, Table, Text } from '@radix-ui/themes'
import { ExclamationTriangleIcon } from '@radix-ui/react-icons'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import type { Server } from '@/domain/server/types'
import type { Integration, Site } from '@/domain/site/types'

/**
 * A single headline count on the overview.
 *
 * Every value shown here is read from the API, never synthesised: if a number cannot be
 * derived from a real listing it is not shown.
 */
function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <Card>
      <Text size="1" color="gray">
        {label}
      </Text>
      <Text as="div" size="7" weight="bold">
        {value}
      </Text>
      {hint && (
        <Text size="1" color="gray">
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

/** Human-readable sync freshness for an integration row. */
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

/**
 * The landing screen: what the platform currently knows, as counts plus integration
 * freshness.
 *
 * Loads one server listing, the sites, and the integrations, then derives counts from
 * them. State is a discriminated union set only from the fetch callbacks, so nothing is
 * written synchronously during the effect. Numbers cover the first page of servers (the
 * API maximum); this is called out when the fleet exceeds it.
 */
export function OverviewPage() {
  const { servers, sites } = useApp()

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

      {failing.map((integration) => (
        <Callout.Root key={integration.id} color="orange" mb="4">
          <Callout.Icon>
            <ExclamationTriangleIcon />
          </Callout.Icon>
          <Callout.Text>
            {integration.name} is not answering: {integration.sync.lastError}
          </Callout.Text>
        </Callout.Root>
      ))}

      <Grid columns={{ initial: '2', md: '5' }} gap="3" mb="4">
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
      </Grid>

      {allServers.length >= 100 && (
        <Text size="1" color="gray" mb="4" as="div">
          Counts cover the first 100 servers. Use the servers page for the full fleet.
        </Text>
      )}

      <Card>
        <Flex justify="between" align="center" mb="3">
          <Heading as="h2" size="3">
            Integrations
          </Heading>
          <Text size="1" color="gray">
            Everything swallow reads comes from one of these
          </Text>
        </Flex>

        {integrations.length === 0 ? (
          <Text size="2" color="gray">
            None registered.
          </Text>
        ) : (
          <Table.Root variant="surface">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Name</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Kind</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Product</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Enabled</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Freshness</Table.ColumnHeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {integrations.map((integration) => (
                <Table.Row key={integration.id}>
                  <Table.Cell>{integration.name}</Table.Cell>
                  <Table.Cell>{integration.kind}</Table.Cell>
                  <Table.Cell>{integration.providerKind}</Table.Cell>
                  <Table.Cell>{integration.enabled ? 'yes' : 'paused'}</Table.Cell>
                  <Table.Cell>
                    <Text size="2" color={integration.sync.lastError ? 'orange' : undefined}>
                      {freshness(integration)}
                    </Text>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        )}
      </Card>
    </>
  )
}
