import { Alert, AlertVariant, Flex, Label, Tab, Tabs, TabTitleText } from '@patternfly/react-core'
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { ProvisioningBadge, HealthBadge } from '@/presentation/components/AxisBadge'
import { serverDisplayName } from '@/domain/server/list'
import { ServerActionMenu } from './ServerActionMenu'
import { useServerDetail } from './useServerDetail'

const TABS = [{ value: 'summary', label: 'Summary' }, { value: 'monitoring', label: 'Monitoring' }, { value: 'network', label: 'Network' }, { value: 'storage', label: 'Storage' }, { value: 'pci', label: 'PCI devices' }]

/**
 * Cockpit-style single-machine route shell. Projection and live provider detail are loaded
 * once and shared through outlet context; every tab remains deep-linkable and horizontally
 * scrollable on narrow screens.
 */
export function ServerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const { scopedHref } = useSiteScope()
  const state = useServerDetail(id)
  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'not-found') return <EmptyState title="Server not found" />
  const { server, detail, reload } = state.data
  const segment = location.pathname.split('/').pop() ?? ''
  const current = TABS.some((tab) => tab.value === segment) ? segment : 'summary'
  return <div className="operator-page">
    <PageHeader title={serverDisplayName(server)} breadcrumbs={[{ label: 'Servers', href: scopedHref('/servers') }, { label: serverDisplayName(server) }]} subtitle={`Provider machine ${server.source.providerMachineId}, Site ${server.source.siteId}`} metadata={<Flex gap={{ default: 'gapSm' }} flexWrap={{ default: 'wrap' }}><ProvisioningBadge axis={server.provisioning} /><HealthBadge axis={server.health} />{server.absent && <Label color="grey">absent</Label>}</Flex>} actions={<ServerActionMenu serverId={server.id} serverName={serverDisplayName(server)} capabilities={detail?.capabilities ?? null} onActed={reload} />} />
    {server.absent && <Alert variant={AlertVariant.warning} title="Machine is absent from its provisioner" isInline>Swallow retains the projection because inventory absence is commonly transient.</Alert>}
    <div className="sw-detail-tabs"><Tabs activeKey={current} onSelect={(_event, key) => navigate(scopedHref(`/servers/${server.id}/${String(key)}`))} aria-label="Server details">{TABS.map((tab) => <Tab key={tab.value} eventKey={tab.value} title={<TabTitleText>{tab.label}</TabTitleText>} />)}</Tabs></div>
    <Outlet context={state.data} />
  </div>
}
