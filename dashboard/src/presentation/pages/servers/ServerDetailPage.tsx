import { Badge, Box, Callout, Flex, Heading, Tabs, Text } from '@radix-ui/themes'
import { ExclamationTriangleIcon } from '@radix-ui/react-icons'
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ProvisioningBadge, HealthBadge } from '@/presentation/components/AxisBadge'
import { serverDisplayName } from '@/domain/server/list'
import { ServerActionMenu } from './ServerActionMenu'
import { useServerDetail } from './useServerDetail'

/** The tabs, in order. Values are the child route path segments. */
const TABS = [
  { value: 'summary', label: 'Summary' },
  { value: 'network', label: 'Network' },
  { value: 'storage', label: 'Storage' },
  { value: 'pci', label: 'PCI devices' },
]

/**
 * The server detail shell: header, tabs, and the routed tab content.
 *
 * Loads the server and its live provisioner detail once and shares them with the tab
 * routes through the outlet context, so switching tabs never refetches. The header shows
 * the two persisted axes (provisioning, health) as badges and the capability-gated action
 * menu; membership and the fuller data live on the Summary tab. Tabs are backed by nested
 * routes so a tab is deep-linkable.
 */
export function ServerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const state = useServerDetail(id)

  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'not-found') return <EmptyState title="Server not found" />

  const { server, detail, reload } = state.data

  // The active tab is the last path segment; default to summary for the bare detail path.
  const segment = location.pathname.split('/').pop() ?? ''
  const currentTab = TABS.some((tab) => tab.value === segment) ? segment : 'summary'

  return (
    <>
      <Flex justify="between" align="start" mb="4" gap="3" wrap="wrap">
        <Flex direction="column" gap="1">
          <Flex align="center" gap="2" wrap="wrap">
            <Heading as="h1" size="6">
              {serverDisplayName(server)}
            </Heading>
            {server.absent && (
              <Badge color="gray" variant="outline">
                absent from provisioner
              </Badge>
            )}
          </Flex>
          <Text color="gray" size="2">
            {server.source.providerMachineId} · site {server.source.siteId}
          </Text>
          <Flex align="center" gap="2" mt="1">
            <ProvisioningBadge axis={server.provisioning} />
            <HealthBadge axis={server.health} />
          </Flex>
        </Flex>

        <ServerActionMenu
          serverId={server.id}
          serverName={serverDisplayName(server)}
          capabilities={detail?.capabilities ?? null}
          onActed={reload}
        />
      </Flex>

      {server.absent && (
        <Callout.Root color="gray" mb="4">
          <Callout.Icon>
            <ExclamationTriangleIcon />
          </Callout.Icon>
          <Callout.Text>
            The provisioner has stopped listing this machine. It is kept rather than deleted,
            because absence is usually transient.
          </Callout.Text>
        </Callout.Root>
      )}

      <Tabs.Root value={currentTab} onValueChange={(value) => navigate(`/servers/${server.id}/${value}`)}>
        <Tabs.List>
          {TABS.map((tab) => (
            <Tabs.Trigger key={tab.value} value={tab.value}>
              {tab.label}
            </Tabs.Trigger>
          ))}
        </Tabs.List>
      </Tabs.Root>

      <Box mt="4">
        <Outlet context={state.data} />
      </Box>
    </>
  )
}
