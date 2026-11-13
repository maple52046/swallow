import { useCallback, useState } from 'react'
import { Badge, Button, HStack, Text } from '@chakra-ui/react'
import { Activity, Boxes, ChartLine, ExternalLink, LayoutDashboard, RefreshCw, SlidersHorizontal } from 'lucide-react'
import { Link, Navigate, Outlet, useLocation, useMatch, useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Operation } from '@/domain/operation/types'
import { platformMemberCounts } from '@/domain/platform/membership'
import { wantsPlatformGrafana } from '@/domain/platform/monitoring'
import type { Platform } from '@/domain/platform/types'
import { canOperatePlatform } from '@/domain/user/access'
import { platformModuleUI } from '@/modules/registry'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { WorkspaceTabs, type WorkspaceTabItem } from '@/presentation/components/WorkspaceTabs'
import { Alert } from '@/presentation/components/ui/alert'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { PlatformLifecycleActions } from './PlatformLifecycleActions'
import { PlatformLifecycleBadge } from './PlatformLifecycleBadge'
import { pendingUninstallNotice, plannedInstallNotice } from './platformSpecState'
import type { PlatformTransition, PlatformWorkspaceContextValue } from './PlatformWorkspaceContext'
import { usePlatformWorkspace } from './usePlatformWorkspace'
import './platformWorkspace.css'

interface WorkspaceLocationState {
  platformTransition?: PlatformTransition
}

/** Redirects the legacy Platform detail URL to its canonical Overview child route. */
export function PlatformWorkspaceIndexRedirect() {
  const { id } = useParams<{ id: string }>()
  const { scopedHref } = useSiteScope()
  const location = useLocation()
  if (!id) return <Navigate to={scopedHref('/platforms')} replace />
  return (
    <Navigate
      to={scopedHref(`/platforms/${encodeURIComponent(id)}/overview`)}
      replace
      state={location.state}
    />
  )
}

/** The shell's own tabs; a Platform module's tabs sit between Overview and these. */
const OVERVIEW_TAB: WorkspaceTabItem = { value: 'overview', label: 'Overview', icon: LayoutDashboard }
const TRAILING_TABS: readonly WorkspaceTabItem[] = [
  { value: 'monitoring', label: 'Monitoring', icon: ChartLine },
  { value: 'configuration', label: 'Configuration', icon: SlidersHorizontal },
  { value: 'activity', label: 'Activity', icon: Activity },
]

/**
 * Route-based Platform runtime shell. It owns only generic identity, lifecycle Workflow, actions,
 * and navigation; every child route loads its own members, history, or module live state.
 *
 * The tabs are the child routes: the first path segment after the Platform id selects one (deeper
 * paths keep their tab), and choosing a tab navigates in the Platform's own Site scope, so the URL
 * stays the only state. A module tab that does not apply to this Platform is not listed; its URL
 * shows the Not Found child with no tab selected.
 */
export function PlatformWorkspacePage() {
  const { id } = useParams<{ id: string }>()
  const location = useLocation()
  const navigate = useNavigate()
  const tabMatch = useMatch('/platforms/:id/:tab/*')
  const { siteId, sites, scopedHref } = useSiteScope()
  const initialTransition = (location.state as WorkspaceLocationState | null)?.platformTransition
  const state = usePlatformWorkspace(id, siteId, initialTransition)
  const { currentUser } = useAuth()

  if (state.status === 'loading') {
    return <><PageHeader title="Platform" /><LoadingState rows={7} /></>
  }
  if (state.status === 'not-found') {
    return <><PageHeader title="Platform" /><EmptyState title="Platform not found" message="This Platform no longer exists." /></>
  }
  if (state.status === 'error') {
    return <><PageHeader title="Platform" /><ErrorState message={state.message} requestId={state.requestId} /></>
  }

  const { platform, lifecycleOperation, lifecycleFailure, transition, reload } = state.data
  const module = platformModuleUI(platform.type)
  const Icon = module?.icon ?? Boxes
  const siteName = sites.find((site) => site.id === platform.siteId)?.name ?? platform.siteId
  const operable = canOperatePlatform(currentUser, platform)
  const nativeRoutes = (module?.tabs ?? []).filter((route) => route.available(platform) && (!route.requiresOperate || operable))
  const context: PlatformWorkspaceContextValue = {
    platform,
    lifecycleOperation,
    lifecycleFailure,
    transition,
    reload,
  }

  return (
    <div className="console-page sw-platform-workspace">
      <div className="sw-platform-workspace__hero">
        <PageHeader
          title={platform.name}
          breadcrumbs={[{ label: 'Platforms', href: scopedHref('/platforms') }]}
          stackActionsOnMobile
          metadata={
            <HStack gap="2" wrap="wrap">
              <span className="sw-platform-workspace__icon" aria-hidden><Icon size={20} /></span>
              <Badge colorPalette={module?.badgePalette ?? 'gray'} variant="subtle">{module?.label ?? platform.type}</Badge>
              <PlatformLifecycleBadge state={platform.lifecycleState} />
              <Text color="fg.muted" fontSize="sm">{siteName}</Text>
            </HStack>
          }
          actions={<WorkspaceActions platform={platform} lifecycleOperation={lifecycleOperation} operable={operable} reload={reload} />}
        />
        <Text className="sw-platform-workspace__summary" color="fg.muted">
          {runtimeSummary(platform, module?.label ?? platform.type)}
        </Text>
      </div>

      <LifecycleNotice platform={platform} transition={transition} />
      {lifecycleFailure && (
        <Alert status="warning" title="Lifecycle Workflow could not be loaded">
          {lifecycleFailure.message}{lifecycleFailure.requestId ? ' Request ID: ' + lifecycleFailure.requestId + '.' : ''}
          The Platform lifecycle remains available.
          <Button variant="plain" size="sm" onClick={() => reload()}><RefreshCw size={14} />Retry</Button>
        </Alert>
      )}

      <WorkspaceTabs
        value={tabMatch?.params.tab ?? ''}
        items={[OVERVIEW_TAB, ...nativeRoutes, ...TRAILING_TABS]}
        label="Platform workspace"
        onChange={(value) => navigate(scopedHref(`/platforms/${encodeURIComponent(platform.id)}/${value}`, platform.siteId))}
      />

      <Outlet context={context} />
    </div>
  )
}

function runtimeSummary(platform: Platform, moduleLabel: string): string {
  const counts = platformMemberCounts(platform)
  if (platform.lifecycleState === 'active') {
    return `${moduleLabel} runtime · ${counts.matched} of ${counts.total} members matched`
  }
  if (platform.lifecycleState === 'deploying') return `${moduleLabel} runtime deployment is in progress`
  if (platform.lifecycleState === 'uninstalling') return `${moduleLabel} runtime is being removed`
  if (platform.lifecycleState === 'deploy_failed' || platform.lifecycleState === 'uninstall_failed') {
    return `${moduleLabel} runtime needs operational review`
  }
  return `${moduleLabel} runtime control plane`
}

/**
 * The header's Workflow and Grafana links, and for a user who may manage the Platform (an admin or an
 * owner of its Pool), Sync and the lifecycle actions.
 */
function WorkspaceActions({
  platform,
  lifecycleOperation,
  operable,
  reload,
}: {
  platform: Platform
  lifecycleOperation?: Operation
  operable: boolean
  reload: (transition?: PlatformTransition) => void
}) {
  const { platforms } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [syncing, setSyncing] = useState(false)
  const failed = platform.lifecycleState === 'deploy_failed' || platform.lifecycleState === 'uninstall_failed'
  const changing = platform.lifecycleState === 'deploying' || platform.lifecycleState === 'uninstalling'
  const grafana = platform.monitoringLinks.find((entry) => entry.name.toLowerCase().includes('grafana'))

  const sync = useCallback(async () => {
    setSyncing(true)
    try {
      const report = await platforms.syncPlatform(platform.id)
      showToast({
        tone: report.error ? 'error' : 'success',
        title: report.error ? 'Sync failed' : 'Membership synced',
        description: report.error ?? `${report.matched} of ${report.members} members matched to Servers.`,
      })
      reload()
    } catch (error) {
      showToast({ tone: 'error', title: 'Sync failed', description: failureText(failureOf(error, 'Could not sync membership.')) })
    } finally {
      setSyncing(false)
    }
  }, [platform.id, platforms, reload, showToast])

  return (
    <>
      {(failed || changing) && lifecycleOperation && (
        <Button asChild colorPalette={failed ? 'red' : 'brand'}>
          <Link to={scopedHref(`/workflows/${lifecycleOperation.id}`, platform.siteId)}>
            {failed ? 'Review workflow' : 'View workflow'}
          </Link>
        </Button>
      )}
      {!failed && !changing && platform.lifecycleState === 'active' && wantsPlatformGrafana(platform.spec) && grafana && (
        <Button asChild colorPalette="brand">
          {/* The visible text is short for the header row; the accessible name keeps what it opens. */}
          <a href={grafana.url} target="_blank" rel="noreferrer" aria-label="Open Platform Grafana">Grafana<ExternalLink size={14} aria-hidden /></a>
        </Button>
      )}
      {operable && (
        <>
          <Button variant="outline" onClick={() => void sync()} loading={syncing} disabled={syncing || !platform.integrationId}>
            <RefreshCw size={16} />Sync
          </Button>
          <PlatformLifecycleActions
            platform={platform}
            operation={lifecycleOperation}
            targetServerIds={lifecycleOperation?.targetServerIds}
            onChanged={reload}
          />
        </>
      )}
    </>
  )
}

function LifecycleNotice({ platform, transition }: { platform: Platform; transition?: PlatformTransition }) {
  const { scopedHref } = useSiteScope()
  if (transition === 'install') {
    return <Alert className="sw-platform-lifecycle-notice" status="info" title="Install is starting">The confirmed Platform deployment is waiting for the controller to observe its new generation.</Alert>
  }
  if (transition === 'uninstall') {
    return <Alert className="sw-platform-lifecycle-notice" status="info" title="Uninstall is starting">The confirmed uninstall is waiting for the controller to observe its new generation.</Alert>
  }

  const pending = pendingUninstallNotice(platform) ?? (platform.lifecycleState === 'planned' ? plannedInstallNotice(platform) : null)
  if (pending) {
    return (
      <Alert className="sw-platform-lifecycle-notice" status={pending.status} title={pending.title}>
        {pending.body}{pending.linksChangePlans && <> <Link to={scopedHref('/change-plans', platform.siteId)}>Open Change Plans</Link></>}
      </Alert>
    )
  }
  if (platform.lifecycleState === 'deploying') {
    return <Alert className="sw-platform-lifecycle-notice" status="info" title="Platform deployment is running">The runtime and membership update automatically as its Workflow advances.</Alert>
  }
  if (platform.lifecycleState === 'uninstalling') {
    return <Alert className="sw-platform-lifecycle-notice" status="warning" title="Platform uninstall is running">The record remains available while original targets are cleaned.</Alert>
  }
  if (platform.lifecycleState === 'deploy_failed' || platform.lifecycleState === 'uninstall_failed') {
    return <Alert className="sw-platform-lifecycle-notice" status="error" title="Platform lifecycle needs review">Open the lifecycle Workflow for the failed Task and its diagnostic output.</Alert>
  }
  if (platform.lifecycleState === 'uninstalled') {
    return <Alert className="sw-platform-lifecycle-notice" status="info" title="Platform is uninstalled">Platform software was removed; this retained record can be deleted without changing hosts.</Alert>
  }
  if (!platform.integrationId) {
    return <Alert className="sw-platform-lifecycle-notice" status="warning" title="Platform is unreachable">No Platform Integration is attached, so runtime membership cannot be read.</Alert>
  }
  return null
}
