import { useState } from 'react'
import { Button } from '@chakra-ui/react'
import { DatabaseZap, RefreshCw } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { LocalRepository } from '@/domain/localRepository/types'
import { DataToolbar } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { ArtifactSourceSection } from './ArtifactSourceSection'
import { InfrastructureHeader } from './InfrastructureHeader'
import { LocalRepositoryContentBrowser } from './LocalRepositoryContentBrowser'
import { LocalRepositorySummary } from './LocalRepositorySummary'
import { RepositoryDeclarationsSection } from './RepositoryDeclarationsSection'
import { RepositoryFilesSection } from './RepositoryFilesSection'
import { RepositoryImagesSection } from './RepositoryImagesSection'
import { RepositorySourcesSection } from './RepositorySourcesSection'
import { useLocalRepository } from './useLocalRepository'
import { UserSourcesDialog } from './UserSourcesDialog'
import { UserSourcesSection } from './UserSourcesSection'

/**
 * Infrastructure → Local repository (`/infrastructure/local-repository`): one Site's Local
 * Repository, its own copy of every artifact a deployment installs, and the setting that decides
 * whether its deployments fetch from it or from the upstreams the modules declare (decision 078).
 *
 * A Local Repository belongs to one Site, so the page needs one: the global Site scope when set,
 * or the only Site of an installation that has one. With several Sites and none scoped it asks for
 * one and sets the URL-owned scope; with no Site it points to the Sites page. Every route it uses
 * requires the `admin` role, which the server enforces.
 */
export function LocalRepositoryPage() {
  const { sites, siteId, loading, setSite, scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const effectiveSiteId = siteId ?? (sites.length === 1 ? sites[0].id : undefined)

  if (effectiveSiteId) return <LocalRepositoryWorkspace key={effectiveSiteId} siteId={effectiveSiteId} />

  return (
    <div className="console-page sw-local-repository">
      <InfrastructureHeader />
      {loading && <LoadingState rows={4} />}
      {!loading && sites.length === 0 && (
        <EmptyState
          title="No Sites yet"
          message="A Local Repository belongs to a Site. Create a Site, then register its repository Integration."
          action={{ label: 'Go to Sites', onClick: () => navigate(scopedHref('/infrastructure/sites')) }}
        />
      )}
      {!loading && sites.length > 1 && (
        <>
          <DataToolbar variant="plain">
            <Select
              aria-label="Site"
              value=""
              placeholder="Choose a Site"
              size="sm"
              width="auto"
              onChange={(value) => setSite(value || undefined)}
              options={sites.map((site) => ({ value: site.id, label: site.name }))}
            />
          </DataToolbar>
          <EmptyState
            title="Choose a Site"
            message="Each Site has its own Local Repository. Choose a Site here or in the Site scope menu to see its sources, sync state, and content."
          />
        </>
      )}
    </div>
  )
}

/**
 * The Local repository workspace of one Site, keyed by the Site so switching scope starts clean.
 *
 * - Loading, a failed first read (with retry), and a Site without a `repository` Integration are
 *   exclusive states; the last explains that the Site's deployments fetch from the upstreams and
 *   opens Create integration on the Integrations page with the Repository role preselected.
 * - When the repository is read, the page shows its status, where deployments fetch artifacts (the
 *   setting, see `ArtifactSourceSection`), the effective sources and their observed state, the
 *   user's sources, the declared files and images with their observed state, the declared Python
 *   packages, and the content browser. It re-reads while anything syncs (see `useLocalRepository`).
 *   While the spec does not serve deployments the worker converges nothing, so the observed states
 *   read as the last observation (`synchronizing`), and nothing polls.
 * - "Sync now" asks the worker to converge now (`POST .../sync`); the request returns at once and
 *   the page keeps re-reading until the worker records its next observation. It is offered only
 *   while the spec serves deployments, since otherwise the worker does not converge the repository.
 * - The setting and "Manage user sources" each write the whole spec with the generation they read;
 *   a stale generation re-reads the repository and each says nothing was saved (see
 *   `ArtifactSourceSection` and `UserSourcesDialog`).
 */
function LocalRepositoryWorkspace({ siteId }: { siteId: string }) {
  const { localRepository } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const { state, reload, replace, followSync } = useLocalRepository(siteId)
  const [requestingSync, setRequestingSync] = useState(false)
  const [managing, setManaging] = useState(false)

  const requestSync = async () => {
    if (requestingSync) return
    setRequestingSync(true)
    try {
      await localRepository.requestSync(siteId)
      followSync()
      showToast({
        tone: 'success',
        title: 'Sync requested',
        description: 'The worker converges the Local Repository now; the states here update as it observes them.',
      })
    } catch (caught) {
      showToast({ tone: 'error', title: 'Sync could not be requested', description: failureText(failureOf(caught, 'The sync request failed.')) })
    } finally {
      setRequestingSync(false)
    }
  }

  const handleSaved = (saved: LocalRepository) => {
    setManaging(false)
    replace(saved)
    showToast({
      tone: 'success',
      title: 'User sources saved',
      description: saved.spec.serveDeployments
        ? 'New sources read Pending until the worker syncs them.'
        : "The worker syncs new sources once this Site's deployments use the local repository.",
    })
  }

  const registerRepository = () => {
    navigate(scopedHref('/infrastructure/integrations', siteId), { state: { createIntegration: 'repository' } })
  }

  return (
    <div className="console-page sw-local-repository">
      <InfrastructureHeader
        actions={
          state.status === 'ready' && state.repository.spec.serveDeployments ? (
            <Button colorPalette="brand" onClick={() => void requestSync()} loading={requestingSync} disabled={requestingSync}>
              <RefreshCw size={16} aria-hidden />
              Sync now
            </Button>
          ) : undefined
        }
      />
      {state.status === 'loading' && <LoadingState rows={6} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />}
      {state.status === 'missing' && (
        <EmptyState
          icon={<DatabaseZap />}
          title="This Site has no Local Repository"
          message="This Site's deployments fetch each package, image, and file from the upstream source its module declares. Register a Repository integration (Pulp) for this Site on the Integrations page to give it one."
          action={{ label: 'Register a repository', onClick: registerRepository }}
        />
      )}
      {state.status === 'ready' && (
        <>
          <LocalRepositorySummary repository={state.repository} refreshError={state.refreshError} />
          <ArtifactSourceSection siteId={siteId} repository={state.repository} onSaved={replace} onConflict={reload} />
          <RepositorySourcesSection sources={state.repository.sources} synchronizing={state.repository.spec.serveDeployments} />
          <UserSourcesSection spec={state.repository.spec} onManage={() => setManaging(true)} />
          <RepositoryFilesSection files={state.repository.files} synchronizing={state.repository.spec.serveDeployments} />
          <RepositoryImagesSection images={state.repository.images} synchronizing={state.repository.spec.serveDeployments} />
          <RepositoryDeclarationsSection python={state.repository.python} />
          <LocalRepositoryContentBrowser siteId={siteId} sources={state.repository.sources} />
          {managing && (
            <UserSourcesDialog
              siteId={siteId}
              repository={state.repository}
              onClose={() => setManaging(false)}
              onSaved={handleSaved}
              onConflict={reload}
            />
          )}
        </>
      )}
    </div>
  )
}
