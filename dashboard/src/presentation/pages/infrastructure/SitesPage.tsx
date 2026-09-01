import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button,
  SearchInput,
  ToolbarItem,
} from '@patternfly/react-core'
import { PlusCircleIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { Link } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Integration, Site } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
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
 * Admin Site registry with Integration counts that make the ownership hierarchy visible.
 * Mutations refresh both this working set and the global Site selector before reporting success.
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
      const [nextSites, integrations] = await Promise.all([
        repository.listSites(),
        repository.listIntegrations(),
      ])
      setState({ status: 'ready', sites: nextSites, integrations })
    } catch (caught) {
      setState({
        status: 'error',
        message: caught instanceof Error ? caught.message : 'Could not load Sites.',
      })
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
    return () => {
      cancelled = true
    }
  }, [repository])

  const visibleSites = useMemo(() => {
    if (state.status !== 'ready') return []
    const needle = query.trim().toLowerCase()
    return state.sites.filter((site) => (
      (!siteId || site.id === siteId) &&
      (!needle || [site.name, site.description, site.id].some((value) => value.toLowerCase().includes(needle)))
    ))
  }, [query, siteId, state])

  const integrationCount = (targetSiteId: string) => state.status === 'ready'
    ? state.integrations.filter((integration) => integration.siteId === targetSiteId).length
    : 0

  const handleSaved = async (saved: Site) => {
    setEditor(null)
    await Promise.all([refreshSites(), load()])
    showToast({
      tone: 'success',
      title: editor === 'create' ? 'Site created' : 'Site updated',
      description: `${saved.name} is available as an infrastructure scope.`,
    })
  }

  const handleDeleted = async (deleted: Site) => {
    setDeleting(null)
    if (siteId === deleted.id) setSite(undefined)
    await Promise.all([refreshSites(), load()])
    showToast({
      tone: 'success',
      title: 'Site deleted',
      description: `${deleted.name} was removed from the Swallow registry.`,
    })
  }

  return (
    <div className="operator-page">
      <InfrastructureHeader actions={<Button icon={<PlusCircleIcon />} onClick={() => setEditor('create')}>Create site</Button>} />
      <DataToolbar variant="plain">
        <ToolbarItem>
          <SearchInput
            value={query}
            onChange={(_event, value) => setQuery(value)}
            onClear={() => setQuery('')}
            placeholder="Search Sites"
            aria-label="Search Sites"
          />
        </ToolbarItem>
      </DataToolbar>
      {state.status === 'loading' && <LoadingState rows={5} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {state.status === 'ready' && visibleSites.length === 0 && (
        <EmptyState
          title="No Sites"
          message={query ? 'No Site matches this search.' : 'Create a Site before registering provider Integrations.'}
          action={!query ? { label: 'Create site', onClick: () => setEditor('create') } : undefined}
        />
      )}
      {state.status === 'ready' && visibleSites.length > 0 && (
        <StickyTableFrame>
          <Table aria-label="Sites" variant="compact">
            <Thead><Tr>
              <Th>Name</Th><Th>Description</Th><Th>Integrations</Th><Th>Updated</Th><Th screenReaderText="Actions" />
            </Tr></Thead>
            <Tbody>{visibleSites.map((site) => (
              <Tr key={site.id} id={`site-${site.id}`}>
                <Td dataLabel="Name"><strong>{site.name}</strong><small className="sw-mono">{site.id}</small></Td>
                <Td dataLabel="Description">{site.description || '-'}</Td>
                <Td dataLabel="Integrations">
                  <Link to={`/infrastructure/integrations?site=${encodeURIComponent(site.id)}`}>{integrationCount(site.id)}</Link>
                </Td>
                <Td dataLabel="Updated">{formatDateTime(site.updatedAt)}</Td>
                <Td isActionCell>
                  <span className="sw-row-actions">
                    <Button variant="link" isInline onClick={() => setEditor(site)}>Edit</Button>
                    <Button variant="link" isInline isDanger onClick={() => setDeleting(site)}>Delete</Button>
                  </span>
                </Td>
              </Tr>
            ))}</Tbody>
          </Table>
        </StickyTableFrame>
      )}
      {editor && (
        <SiteDialog
          site={editor === 'create' ? undefined : editor}
          onClose={() => setEditor(null)}
          onSaved={(saved) => void handleSaved(saved)}
        />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel="Site"
          name={deleting.name}
          warning="A Site with registered Integrations cannot be deleted. Remove those connections first."
          onClose={() => setDeleting(null)}
          onDelete={() => repository.deleteSite(deleting.id)}
          onDeleted={() => void handleDeleted(deleting)}
        />
      )}
    </div>
  )
}
