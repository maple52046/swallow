import { useEffect } from 'react'
import { Alert, AlertVariant, Button, Flex, Label, Tab, Tabs, TabTitleText } from '@patternfly/react-core'
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useApp } from '@/di/AppProvider'
import { DeploymentBadge, HealthBadge, LockBadge } from '@/presentation/components/AxisBadge'
import { serverDisplayName } from '@/domain/server/list'
import { ServerActionMenu } from './ServerActionMenu'
import { useServerDetail } from './useServerDetail'

const TABS = [{ value: 'summary', label: 'Summary' }, { value: 'activity', label: 'Activity' }, { value: 'monitoring', label: 'Monitoring' }, { value: 'network', label: 'Network' }, { value: 'storage', label: 'Storage' }, { value: 'pci', label: 'PCI devices' }]

/**
 * Cockpit-style single-machine route shell. Projection and live provider detail are loaded
 * once and shared through outlet context; every tab remains deep-linkable and horizontally
 * scrollable on narrow screens.
 */
export function ServerDetailPage() {
  const { servers } = useApp()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const location = useLocation()
  const { scopedHref } = useSiteScope()
  const state = useServerDetail(id)
  const activeProjection = state.status === 'ready' && (
    ['deploying', 'releasing', 'commissioning', 'testing'].includes(state.data.server.provisioning?.state ?? '') ||
    ['deploying', 'verifying'].includes(state.data.server.deployment?.state ?? '')
  )
  useEffect(() => {
    if (!activeProjection || state.status !== 'ready') return
    let canceled = false
    const timer = window.setInterval(() => {
      void servers.refreshServer(state.data.server.id).then(() => {
        if (!canceled) state.data.reload()
      }).catch(() => undefined)
    }, 2_000)
    void servers.refreshServer(state.data.server.id).then(() => {
      if (!canceled) state.data.reload()
    }).catch(() => undefined)
    return () => {
      canceled = true
      window.clearInterval(timer)
    }
  }, [activeProjection, servers, state])
  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'not-found') return <EmptyState title="Server not found" />
  const { server, detail, reload } = state.data
  const segment = location.pathname.split('/').pop() ?? ''
  const current = TABS.some((tab) => tab.value === segment) ? segment : 'summary'
  const deployDisabledReason = server.absent
    ? 'Server is absent'
    : server.provisioning?.locked
      ? 'Unlock the Server before deployment'
      : server.provisioning?.state !== 'ready'
        ? 'Server must be ready'
        : undefined
  return <div className="operator-page">
    <PageHeader title={serverDisplayName(server)} breadcrumbs={[{ label: 'Servers', href: scopedHref('/servers') }, { label: serverDisplayName(server) }]} subtitle={`Provider machine ${server.source.providerMachineId}, Site ${server.source.siteId}`} metadata={<Flex gap={{ default: 'gapSm' }} flexWrap={{ default: 'wrap' }}><DeploymentBadge axis={server.deployment} provider={server.provisioning} /><LockBadge locked={server.provisioning?.locked ?? false} /><HealthBadge axis={server.health} />{activeProjection && <Label color="blue">Updating...</Label>}{server.absent && <Label color="grey">absent</Label>}</Flex>} actions={<ServerActionMenu server={server} capabilities={detail?.capabilities ?? null} deployDisabledReason={deployDisabledReason} onActed={() => reload()} />} />
    {server.absent && <Alert variant={AlertVariant.warning} title="Machine is absent from its provisioner" isInline>Swallow retains the projection because inventory absence is commonly transient.</Alert>}
    {server.deployment && ['failed', 'requires_attention'].includes(server.deployment.state) && (
      <Alert
        variant={server.deployment.state === 'failed' ? AlertVariant.danger : AlertVariant.warning}
        title={server.deployment.state === 'failed' ? 'Operating system deployment failed' : 'Operating system deployment requires attention'}
        isInline
      >
        {server.deployment.statusReason || 'Swallow could not verify this deployment.'}{' '}
        <Button variant="link" isInline onClick={() => navigate(scopedHref(`/operations/${server.deployment?.operationId}`))}>
          View operation
        </Button>
      </Alert>
    )}
    <div className="sw-detail-tabs"><Tabs activeKey={current} onSelect={(_event, key) => navigate(scopedHref(`/servers/${server.id}/${String(key)}`))} aria-label="Server details">{TABS.map((tab) => <Tab key={tab.value} eventKey={tab.value} title={<TabTitleText>{tab.label}</TabTitleText>} />)}</Tabs></div>
    <Outlet context={state.data} />
  </div>
}
