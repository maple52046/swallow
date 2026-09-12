import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Integration, Site } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { InfrastructureHeader } from './InfrastructureHeader'
import { ResourceDeleteDialog } from './ResourceDeleteDialog'
import { SiteDialog } from './SiteDialog'

type SitesState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; sites: Site[]; integrations: Integration[] }

/**
 * Admin Site registry with the Integration ownership count beside each location.
 *
 * Desktop and mobile representations share the same filtered working set and
 * mutation callbacks. Saves refresh both the table and the global Site picker.
 */
export function SitesPage() {
  const { sites: repository } = useApp()
  const { showToast } = useToast()
  const { siteId, refreshSites, setSite } = useSiteScope()
  const [state, setState] = useState<SitesState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [editor, setEditor] = useState<Site | 'create' | null>(null)
  const [deleting, setDeleting] = useState<Site | null>(null)

  const load = useCallback(async () => {
    try {
      const [nextSites, integrations] = await Promise.all([repository.listSites(), repository.listIntegrations()])
      setState({ status: 'ready', sites: nextSites, integrations })
    } catch (caught) {
      setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load sites' })
    }
  }, [repository])

  useEffect(() => {
    let cancelled = false
    Promise.all([repository.listSites(), repository.listIntegrations()])
      .then(([nextSites, integrations]) => {
        if (!cancelled) setState({ status: 'ready', sites: nextSites, integrations })
      })
      .catch((caught: Error) => {
        if (!cancelled) setState({ status: 'error', message: caught.message })
      })
    return () => { cancelled = true }
  }, [repository])

  const visibleSites = useMemo(() => {
    if (state.status !== 'ready') return []
    const needle = query.trim().toLowerCase()
    return state.sites.filter(
      (site) =>
        (!siteId || site.id === siteId) &&
        (!needle || [site.name, site.description, site.id].some((value) => value.toLowerCase().includes(needle))),
    )
  }, [query, siteId, state])

  const integrationCount = (targetSiteId: string) =>
    state.status === 'ready' ? state.integrations.filter((integration) => integration.siteId === targetSiteId).length : 0

  const handleSaved = async () => {
    const created = editor === 'create'
    setEditor(null)
    await Promise.all([refreshSites(), load()])
    showToast({ tone: 'success', title: created ? 'Site created' : 'Site updated' })
  }

  const handleDeleted = async (deleted: Site) => {
    setDeleting(null)
    if (siteId === deleted.id) setSite(undefined)
    await Promise.all([refreshSites(), load()])
    showToast({ tone: 'success', title: 'Site deleted' })
  }

  return (
    <div className="operator-page">
      <InfrastructureHeader
        actions={
          <Button colorPalette="brand" onClick={() => setEditor('create')}>
            <Plus size={16} />
            Create site
          </Button>
        }
      />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder="Search sites" aria-label="Search sites" />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={5} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {state.status === 'ready' && visibleSites.length === 0 && (
        <EmptyState
          title="No sites"
          message={query ? 'No results match this search.' : 'Create a site before adding integrations.'}
          action={!query ? { label: 'Create site', onClick: () => setEditor('create') } : undefined}
        />
      )}
      {state.status === 'ready' && visibleSites.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Sites">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Description</Table.ColumnHeader>
                    <Table.ColumnHeader>Integrations</Table.ColumnHeader>
                    <Table.ColumnHeader>Updated</Table.ColumnHeader>
                    <Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleSites.map((site) => (
                    <Table.Row key={site.id} id={`site-${site.id}`}>
                      <Table.Cell>
                        <strong>{site.name}</strong>
                        <Text as="small" display="block" color="fg.muted" className="sw-mono">{site.id}</Text>
                      </Table.Cell>
                      <Table.Cell>{site.description || '-'}</Table.Cell>
                      <Table.Cell><Link to={`/infrastructure/integrations?site=${encodeURIComponent(site.id)}`}>{integrationCount(site.id)}</Link></Table.Cell>
                      <Table.Cell>{formatDateTime(site.updatedAt)}</Table.Cell>
                      <Table.Cell textAlign="end">
                        <span className="sw-row-actions">
                          <Button variant="plain" size="sm" px="1" h="auto" colorPalette="brand" onClick={() => setEditor(site)}>Edit</Button>
                          <Button variant="plain" size="sm" px="1" h="auto" colorPalette="red" onClick={() => setDeleting(site)}>Delete</Button>
                        </span>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Sites">
              {visibleSites.map((site) => (
                <ResourceCard
                  key={site.id}
                  title={site.name}
                  description={site.description || site.id}
                  actions={
                    <>
                      <Button variant="outline" size="sm" onClick={() => setEditor(site)}>Edit</Button>
                      <Button variant="outline" size="sm" colorPalette="red" onClick={() => setDeleting(site)}>Delete</Button>
                    </>
                  }
                >
                  <ResourceCardField label="Integrations"><Link to={`/infrastructure/integrations?site=${encodeURIComponent(site.id)}`}>{integrationCount(site.id)}</Link></ResourceCardField>
                  <ResourceCardField label="Updated">{formatDateTime(site.updatedAt)}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}

      {editor && (
        <SiteDialog
          site={editor === 'create' ? undefined : editor}
          onClose={() => setEditor(null)}
          onSaved={() => void handleSaved()}
        />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel="Site"
          name={deleting.name}
          warning="Remove its integrations first."
          onClose={() => setDeleting(null)}
          onDelete={() => repository.deleteSite(deleting.id)}
          onDeleted={() => void handleDeleted(deleting)}
        />
      )}
    </div>
  )
}
