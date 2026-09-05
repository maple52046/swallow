import { useEffect, useRef, useState } from 'react'
import { Alert, AlertVariant, Flex, Label, Tab, Tabs, TabTitleText } from '@patternfly/react-core'
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
import { DeploymentFailureAlert } from './DeploymentFailureAlert'
import { useServerDetail } from './useServerDetail'

const TABS = [{ value: 'summary', label: 'Summary' }, { value: 'activity', label: 'Activity' }, { value: 'monitoring', label: 'Monitoring' }, { value: 'network', label: 'Network' }, { value: 'storage', label: 'Storage' }, { value: 'pci', label: 'PCI devices' }]

const RELEASE_FOLLOW_INTERVAL_MS = 2_000
const RELEASE_FOLLOW_MAX_ATTEMPTS = 150

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
  // A just-accepted release runs as an asynchronous durable Operation, so the Server is not
  // yet in an active provisioning axis and the active-projection poll below will not pick it
  // up. Follow it until it transitions, then hand off to that poll.
  const [releaseFollow, setReleaseFollow] = useState<{ id: string; token: number } | null>(null)
  // Stable handle to the latest reload so the follow effect need not depend on `state`
  // (which changes on every reload, and would otherwise restart the follow endlessly).
  const reloadRef = useRef<() => void>(() => {})
  useEffect(() => {
    if (state.status === 'ready') reloadRef.current = state.data.reload
  })
  // Whether the followed Server has already entered an active axis. Tracked in a ref (not
  // state) so the hand-off does not setState inside the effect, and so the follow does not
  // restart once the active-projection poll takes over and the Server later converges.
  const releaseHandedOffRef = useRef(false)
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
  useEffect(() => {
    if (!releaseFollow) return
    if (activeProjection) {
      // The Server entered an active axis; the active-projection effect now drives it to
      // convergence. Record the hand-off so this effect does not resume polling once the
      // Server later leaves that axis (converges to ready).
      releaseHandedOffRef.current = true
      return
    }
    if (releaseHandedOffRef.current) return
    const targetId = releaseFollow.id
    let canceled = false
    let attempts = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      await servers.refreshServer(targetId).catch(() => undefined)
      if (canceled) return
      reloadRef.current()
      attempts += 1
      if (attempts < RELEASE_FOLLOW_MAX_ATTEMPTS) {
        timer = setTimeout(() => void tick(), RELEASE_FOLLOW_INTERVAL_MS)
      } else {
        setReleaseFollow(null)
      }
    }
    void tick()
    return () => {
      canceled = true
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [releaseFollow, activeProjection, servers])
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
    <PageHeader title={serverDisplayName(server)} breadcrumbs={[{ label: 'Servers', href: scopedHref('/servers') }, { label: serverDisplayName(server) }]} subtitle={`Provider machine ${server.source.providerMachineId}, Site ${server.source.siteId}`} metadata={<Flex gap={{ default: 'gapSm' }} flexWrap={{ default: 'wrap' }}><DeploymentBadge axis={server.deployment} provider={server.provisioning} /><LockBadge locked={server.provisioning?.locked ?? false} /><HealthBadge axis={server.health} />{activeProjection && <Label color="blue">Updating...</Label>}{server.absent && <Label color="grey">absent</Label>}</Flex>} actions={<ServerActionMenu server={server} capabilities={detail?.capabilities ?? null} deployDisabledReason={deployDisabledReason} onActed={(action) => { reload(); if (action === 'release') { releaseHandedOffRef.current = false; setReleaseFollow({ id: server.id, token: Date.now() }) } }} />} />
    {server.absent && <Alert variant={AlertVariant.warning} title="Machine is absent from its provisioner" isInline>Swallow retains the projection because inventory absence is commonly transient.</Alert>}
    {server.deployment && ['failed', 'requires_attention'].includes(server.deployment.state) && (
      <DeploymentFailureAlert
        deployment={server.deployment}
        onViewOperation={() => navigate(scopedHref(`/operations/${server.deployment?.operationId}`))}
      />
    )}
    <div className="sw-detail-tabs"><Tabs activeKey={current} onSelect={(_event, key) => navigate(scopedHref(`/servers/${server.id}/${String(key)}`))} aria-label="Server details">{TABS.map((tab) => <Tab key={tab.value} eventKey={tab.value} title={<TabTitleText>{tab.label}</TabTitleText>} />)}</Tabs></div>
    <Outlet context={state.data} />
  </div>
}
