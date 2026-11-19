import { Badge, Box, Button, Heading, HStack, Stack, Text } from '@chakra-ui/react'
import { Plus, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Pool } from '@/domain/infrastructure/types'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import type { ProvisionerImageCatalog } from '@/domain/site/types'
import { canOperatePool, isAdmin, ownsAnyPool, poolRoleOf } from '@/domain/user/access'
import { useApp } from '@/di/AppProvider'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { InventorySurface } from '@/presentation/components/ConsolePrimitives'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { DebouncedSearchInput } from '@/presentation/components/ui/debounced-search-input'
import { Modal } from '@/presentation/components/ui/modal'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useToast } from '@/presentation/components/toast/toastContext'
import { formatDateTime } from '@/shared/utils/time'
import { DeploymentTemplateCard } from './DeploymentTemplateCard'
import {
  DeploymentTemplateDialog,
  type DeploymentTemplateEditorTarget,
} from './DeploymentTemplateDialog'
import {
  deploymentTemplateMatches,
  groupDeploymentTemplates,
  resolveTemplateImage,
  templateIntegrationName,
} from './deploymentTemplatePresentation'
import './deploymentTemplates.css'

interface TemplateWorkspaceData {
  templates: readonly DeploymentTemplate[]
  catalogs: readonly ProvisionerImageCatalog[]
  pools: readonly Pool[]
  refreshedAt: Date
  catalogFailure: DisplayedFailure | null
  poolFailure: DisplayedFailure | null
}

type TemplateWorkspaceState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; data: TemplateWorkspaceData; refreshing: boolean; refreshFailure: DisplayedFailure | null }

/**
 * Pool-grouped Deployment Template catalog.
 *
 * Templates are the primary read and establish whether the route exists; image catalogs and the
 * Pool directory are secondary context, so their failures remain warnings over readable cards.
 * Every Site change invalidates the previous working set, while manual refresh preserves the last
 * good cards. Create/edit is delegated to one two-step dialog and never turns a Template into an
 * executable deployment shortcut.
 */
export function DeploymentTemplatesPage() {
  const { provisioning, infrastructure } = useApp()
  const { currentUser } = useAuth()
  const { siteId, sites, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<TemplateWorkspaceState>({ status: 'loading' })
  const [editor, setEditor] = useState<DeploymentTemplateEditorTarget | null>(null)
  const [deleting, setDeleting] = useState<DeploymentTemplate | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteFailure, setDeleteFailure] = useState<DisplayedFailure | null>(null)
  const generation = useRef(0)
  const lastGood = useRef<TemplateWorkspaceData | null>(null)
  const query = searchParams.get('q') ?? ''

  const load = useCallback(async (preserve: boolean) => {
    const request = ++generation.current
    const previous = preserve ? lastGood.current : null
    if (preserve && previous) {
      setState({ status: 'ready', data: previous, refreshing: true, refreshFailure: null })
    } else {
      setState({ status: 'loading' })
    }

    const [templatesResult, catalogsResult, poolsResult] = await Promise.allSettled([
      provisioning.listTemplates({ siteId }),
      provisioning.listOSImageCatalogs(siteId),
      infrastructure.listPools(),
    ])
    if (request !== generation.current) return

    if (templatesResult.status === 'rejected') {
      const failure = failureOf(templatesResult.reason, 'Could not load deployment templates.')
      if (previous) {
        setState({ status: 'ready', data: previous, refreshing: false, refreshFailure: failure })
      } else {
        setState({ status: 'error', ...failure })
      }
      return
    }

    const data: TemplateWorkspaceData = {
      templates: templatesResult.value,
      catalogs: catalogsResult.status === 'fulfilled' ? catalogsResult.value : previous?.catalogs ?? [],
      pools: poolsResult.status === 'fulfilled' ? poolsResult.value : previous?.pools ?? [],
      refreshedAt: new Date(),
      catalogFailure: catalogsResult.status === 'rejected'
        ? failureOf(catalogsResult.reason, 'OS image catalogs could not be refreshed.')
        : null,
      poolFailure: poolsResult.status === 'rejected'
        ? failureOf(poolsResult.reason, 'The Pool directory could not be refreshed.')
        : null,
    }
    lastGood.current = data
    setState({ status: 'ready', data, refreshing: false, refreshFailure: null })
  }, [infrastructure, provisioning, siteId])

  // Site scope owns the working set. Cleanup invalidates any slower response from the old scope.
  useEffect(() => {
    lastGood.current = null
    setEditor(null)
    void load(false)
    return () => {
      generation.current += 1
    }
  }, [load])

  const closeEditor = useCallback(() => {
    setEditor(null)
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('create')
      next.delete('integrationId')
      next.delete('imageId')
      return next
    }, { replace: true })
  }, [setSearchParams])

  // OS Images deep-links into the same create flow. Waiting for the workspace guarantees the
  // preselected Integration and image come from the exact Site-scoped catalogs the dialog shows.
  useEffect(() => {
    if (state.status !== 'ready' || editor || searchParams.get('create') !== '1') return
    setEditor({
      kind: 'create',
      integrationId: searchParams.get('integrationId') ?? '',
      imageId: searchParams.get('imageId') ?? '',
    })
  }, [editor, searchParams, state])

  const commitSearch = useCallback((value: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      if (value) next.set('q', value)
      else next.delete('q')
      return next
    })
  }, [setSearchParams])

  const data = state.status === 'ready' ? state.data : null
  const filtered = useMemo(
    () => data?.templates.filter((template) => deploymentTemplateMatches(template, query, data.catalogs, data.pools)) ?? [],
    [data, query],
  )
  const groups = useMemo(() => groupDeploymentTemplates(filtered, data?.pools ?? []), [data?.pools, filtered])
  const poolCount = useMemo(
    () => new Set((data?.templates ?? []).flatMap((template) => template.poolId ? [template.poolId] : [])).size,
    [data?.templates],
  )
  const admin = isAdmin(currentUser)
  const creatablePools = data?.pools.filter((pool) => admin || poolRoleOf(currentUser, pool.id) === 'owner') ?? []
  const usableCatalogs = data?.catalogs.filter((catalog) => !catalog.error) ?? []
  const canOfferCreate = ownsAnyPool(currentUser)
  const createBlocked = creatablePools.length === 0
    ? 'A writable Pool is required to create a Template.'
    : usableCatalogs.length === 0
      ? 'A readable provisioner image catalog is required to create a Template.'
      : ''
  const embeddedCatalogFailures = data?.catalogs.filter((catalog) => catalog.error) ?? []

  const removeTemplate = async () => {
    if (!deleting || deleteBusy) return
    setDeleteBusy(true)
    setDeleteFailure(null)
    try {
      await provisioning.deleteTemplate(deleting.id)
      showToast({ tone: 'success', title: 'Deployment template deleted', description: deleting.name })
      setDeleting(null)
      await load(true)
    } catch (caught) {
      setDeleteFailure(failureOf(caught, 'The deployment template could not be deleted.'))
    } finally {
      setDeleteBusy(false)
    }
  }

  const openCreate = () => setEditor({ kind: 'create', integrationId: '', imageId: '' })

  return (
    <div className="console-page sw-template-catalog-page">
      <PageHeader
        title="Deployment Templates"
        subtitle="Reusable OS deployment recipes for a Pool: image, deploy mode, network intent, and optional preparation."
        stackActionsOnMobile
        actions={(
          <>
            <Button variant="outline" onClick={() => void load(true)} loading={state.status === 'ready' && state.refreshing} disabled={state.status === 'loading'}>
              <RefreshCw size={15} aria-hidden />
              Refresh
            </Button>
            {canOfferCreate && (
              <Button colorPalette="brand" onClick={openCreate} disabled={Boolean(createBlocked)} title={createBlocked || undefined}>
                <Plus size={16} aria-hidden />
                Create template
              </Button>
            )}
          </>
        )}
      />

      {state.status === 'ready' && state.refreshFailure && (
        <Alert status="warning" title="Refresh failed" actions={<Button size="xs" variant="outline" onClick={() => void load(true)}>Try again</Button>}>
          <FailureMessage failure={state.refreshFailure} /> The last successful catalog remains visible.
        </Alert>
      )}
      {state.status === 'ready' && state.data.catalogFailure && (
        <Alert status="warning" title="Image context unavailable"><FailureMessage failure={state.data.catalogFailure} /> Template records remain available.</Alert>
      )}
      {state.status === 'ready' && state.data.poolFailure && (
        <Alert status="warning" title="Pool directory unavailable"><FailureMessage failure={state.data.poolFailure} /> Pool ids remain visible, but creating a Template is unavailable.</Alert>
      )}
      {state.status === 'ready' && embeddedCatalogFailures.length > 0 && (
        <Alert status="warning" title={`${embeddedCatalogFailures.length} image ${embeddedCatalogFailures.length === 1 ? 'catalog is' : 'catalogs are'} unavailable`}>
          Template cards distinguish provider read failures from images that are confirmed missing.
        </Alert>
      )}

      {state.status === 'loading' && <LoadingState rows={6} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => void load(false)} />}
      {state.status === 'ready' && (
        <InventorySurface
          headingId="deployment-template-catalog-heading"
          eyebrow="OS deployment recipes"
          title="Reusable configurations"
          summary={(
            <Stack gap="0.5" alignItems={{ base: 'flex-start', md: 'flex-end' }}>
              <Text>{`Showing ${filtered.length} of ${state.data.templates.length} templates across ${poolCount} ${poolCount === 1 ? 'Pool' : 'Pools'}`}</Text>
              <Text fontSize="xs">Updated {formatDateTime(state.data.refreshedAt.toISOString())}</Text>
            </Stack>
          )}
          toolbar={(
            <DebouncedSearchInput
              value={query}
              onCommit={commitSearch}
              placeholder="Search template, image, Pool, or Platform"
              aria-label="Search deployment templates"
              maxW="30rem"
              size="md"
            />
          )}
          className="sw-template-inventory"
        >
          {state.data.templates.length === 0 ? (
            <EmptyState
              title="No deployment templates"
              message={canOfferCreate ? 'Create the first reusable OS deployment recipe for one of your Pools.' : 'None of your Pools currently provides a deployment Template.'}
              action={canOfferCreate && !createBlocked ? { label: 'Create template', onClick: openCreate } : undefined}
            />
          ) : filtered.length === 0 ? (
            <EmptyState
              title="No matching templates"
              message={`No deployment Template matches “${query}”.`}
              action={{ label: 'Clear search', onClick: () => commitSearch('') }}
            />
          ) : (
            <Stack gap="8" className="sw-template-groups">
              {groups.map((group) => (
                <Box as="section" key={group.key} aria-labelledby={`template-pool-${group.key}`}>
                  <HStack className="sw-template-group__heading" gap="3">
                    <Heading as="h3" id={`template-pool-${group.key}`} size="md">{group.label}</Heading>
                    <Badge variant="subtle" colorPalette="gray">{group.templates.length}</Badge>
                  </HStack>
                  <div className="sw-template-card-grid">
                    {group.templates.map((template) => (
                      <DeploymentTemplateCard
                        key={template.id}
                        template={template}
                        image={resolveTemplateImage(template, state.data.catalogs)}
                        poolName={group.label}
                        integrationName={templateIntegrationName(template, state.data.catalogs)}
                        siteName={sites.find((site) => site.id === template.siteId)?.name ?? template.siteId}
                        showSite={!siteId}
                        canChange={canOperatePool(currentUser, template.poolId)}
                        scopedHref={scopedHref}
                        onEdit={() => setEditor({ kind: 'edit', template })}
                        onDelete={() => { setDeleteFailure(null); setDeleting(template) }}
                      />
                    ))}
                  </div>
                </Box>
              ))}
            </Stack>
          )}
        </InventorySurface>
      )}

      {editor && data && (
        <DeploymentTemplateDialog
          key={editor.kind === 'edit' ? editor.template.id : `create-${editor.integrationId}-${editor.imageId}`}
          target={editor}
          catalogs={data.catalogs}
          pools={data.pools}
          currentUser={currentUser}
          onClose={closeEditor}
          onChanged={() => load(true)}
        />
      )}

      <Modal
        open={Boolean(deleting)}
        onClose={() => { setDeleting(null); setDeleteFailure(null) }}
        title="Delete deployment template?"
        description={deleting ? `${deleting.name} will no longer be available for future OS or Platform deployments.` : undefined}
        role="alertdialog"
        dismissDisabled={deleteBusy}
        footer={(
          <>
            <Button variant="ghost" onClick={() => { setDeleting(null); setDeleteFailure(null) }} disabled={deleteBusy}>Cancel</Button>
            <Button colorPalette="red" loading={deleteBusy} disabled={deleteBusy} onClick={() => void removeTemplate()}>Delete template</Button>
          </>
        )}
      >
        {deleteFailure ? (
          <Alert status="error" title="Could not delete template"><FailureMessage failure={deleteFailure} /></Alert>
        ) : (
          <Text>This cannot be undone. Existing Servers are not changed.</Text>
        )}
      </Modal>
    </div>
  )
}
