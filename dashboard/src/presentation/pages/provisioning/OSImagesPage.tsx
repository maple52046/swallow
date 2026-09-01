import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  SearchInput,
  ToolbarItem,
} from '@patternfly/react-core'
import { SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { Link, useNavigate } from 'react-router-dom'
import {
  loadOSImageCatalog,
  type OSImageCatalog,
} from '@/application/usecases/provisioning/loadOSImageCatalog'
import { useApp } from '@/di/AppProvider'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { ProvisioningTabs } from './ProvisioningTabs'

type CatalogState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: OSImageCatalog }

function provisioningHref(
  path: string,
  params: Record<string, string>,
  scopedHref: (path: string) => string,
): string {
  const target = new URL(scopedHref(path), window.location.origin)
  for (const [key, value] of Object.entries(params)) target.searchParams.set(key, value)
  return `${target.pathname}${target.search}`
}

/** Read-only live image catalog across scoped provisioner integrations. */
export function OSImagesPage() {
  const { sites: siteRepository, provisioning } = useApp()
  const { sites, siteId, scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const [state, setState] = useState<CatalogState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [refreshNonce, setRefreshNonce] = useState(0)

  const fetchCatalog = useCallback(async () => {
    const integrations = await siteRepository.listIntegrations({
      siteId,
      kind: 'provisioner',
    })
    return loadOSImageCatalog(provisioning, integrations)
  }, [provisioning, siteId, siteRepository])

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      setState({ status: 'ready', data: await fetchCatalog() })
    } catch (error) {
      setState({
        status: 'error',
        message: error instanceof Error ? error.message : 'Could not load OS images.',
      })
    }
  }, [fetchCatalog])

  useEffect(() => {
    let cancelled = false
    fetchCatalog()
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setState({ status: 'error', message: error.message })
        }
      })
    return () => {
      cancelled = true
    }
  }, [fetchCatalog, refreshNonce])

  const items = useMemo(
    () => state.status === 'ready' ? state.data.images : [],
    [state],
  )
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return items
    return items.filter((item) => [
      item.name,
      item.id,
      item.osSystem,
      item.release,
      item.architecture,
      item.integrationName,
    ].some((value) => value.toLowerCase().includes(needle)))
  }, [items, query])

  const siteName = (id: string) => sites.find((site) => site.id === id)?.name ?? id

  return <div className="operator-page">
    <PageHeader
      title="OS images"
      subtitle="Live, read-only images offered by provisioner Integrations registered under each Site."
      breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/deploy') }, { label: 'OS images' }]}
      actions={<>
        <Button variant="secondary" onClick={() => navigate(scopedHref('/infrastructure/integrations'))}>Manage integrations</Button>
        <Button variant="secondary" icon={<SyncAltIcon />} onClick={() => setRefreshNonce((value) => value + 1)}>Refresh</Button>
      </>}
    />
    <ProvisioningTabs />
    <DataToolbar variant="plain">
      <ToolbarItem>
        <SearchInput
          value={query}
          onChange={(_event, value) => setQuery(value)}
          onClear={() => setQuery('')}
          placeholder="Search image, OS, release, or integration"
          aria-label="Search OS images"
        />
      </ToolbarItem>
    </DataToolbar>
    {state.status === 'loading' && <LoadingState rows={7} />}
    {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
    {state.status === 'ready' && state.data.failures.map((failure) => (
      <Alert
        key={failure.integrationId}
        variant={AlertVariant.warning}
        title={`${failure.integrationName} image catalog unavailable`}
        isInline
      >
        {failure.message}
      </Alert>
    ))}
    {state.status === 'ready' && filtered.length === 0 && (
      <EmptyState
        title="No OS images"
        message={query ? 'No live image matches this search.' : 'No scoped provisioner returned an image.'}
      />
    )}
    {state.status === 'ready' && filtered.length > 0 && (
      <StickyTableFrame>
        <Table aria-label="OS images" variant="compact" className="sw-provisioning-table">
          <Thead><Tr>
            <Th>Image</Th><Th>Image ID</Th><Th>OS</Th><Th>Release</Th>
            <Th>Architecture</Th><Th>Site</Th><Th>Provider integration</Th><Th>Refreshed</Th>
            <Th screenReaderText="Actions" />
          </Tr></Thead>
          <Tbody>{filtered.map((image) => (
            <Tr key={`${image.integrationId}:${image.id}:${image.architecture}`}>
              <Td dataLabel="Image"><strong>{image.name || '-'}</strong></Td>
              <Td dataLabel="Image ID" className="sw-mono">{image.id || '-'}</Td>
              <Td dataLabel="OS">{image.osSystem || '-'}</Td>
              <Td dataLabel="Release">{image.release || '-'}</Td>
              <Td dataLabel="Architecture">{image.architecture || '-'}</Td>
              <Td dataLabel="Site">
                <Link to={`/infrastructure/sites?site=${encodeURIComponent(image.siteId)}#site-${image.siteId}`}>
                  {siteName(image.siteId)}
                </Link>
              </Td>
              <Td dataLabel="Provider integration">
                <Link to={`/infrastructure/integrations?site=${encodeURIComponent(image.siteId)}#integration-${image.integrationId}`}>
                  {image.integrationName}
                </Link>
              </Td>
              <Td dataLabel="Refreshed">{formatDateTime(image.refreshedAt)}</Td>
              <Td isActionCell>
                <span className="sw-row-actions">
                  <Button
                    variant="link"
                    isInline
                    onClick={() => navigate(provisioningHref('/provisioning/deploy', {
                      integrationId: image.integrationId,
                      imageId: image.id,
                    }, scopedHref))}
                  >
                    Deploy
                  </Button>
                  <Button
                    variant="link"
                    isInline
                    onClick={() => navigate(provisioningHref('/provisioning/templates', {
                      create: '1',
                      integrationId: image.integrationId,
                      imageId: image.id,
                    }, scopedHref))}
                  >
                    Create template
                  </Button>
                </span>
              </Td>
            </Tr>
          ))}</Tbody>
        </Table>
      </StickyTableFrame>
    )}
  </div>
}
