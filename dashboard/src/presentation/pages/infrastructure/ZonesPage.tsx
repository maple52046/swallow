import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { Zone } from '@/domain/infrastructure/types'
import { ConvergenceStatus } from '@/presentation/components/convergence/ConvergenceStatus'
import { describeConvergence, isSettling } from '@/presentation/components/convergence/convergencePresentation'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { ResourceRowActions, type ResourceRowAction } from '@/presentation/components/ResourceRowActions'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'
import { formatDateTime } from '@/shared/utils/time'
import { InfrastructureHeader } from './InfrastructureHeader'
import { ResourceDeleteDialog } from './ResourceDeleteDialog'
import { ZoneDialog } from './ZoneDialog'

type ZonesState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; zones: Zone[] }

/**
 * Admin registry for swallow-owned Zones (decision 071), route `/infrastructure/zones`. Results
 * honor the global Site scope, and each row shows whether the Site's provisioners hold the Zone as
 * specified. Desktop and mobile share the same filtered working set and mutation callbacks; a save
 * or delete refreshes the list, and while any Zone is still being applied the list is re-read
 * quietly so its status settles in place (a failed re-read keeps the last list). Pools are not
 * here: they belong to the installation and are managed on the Pools page.
 */
export function ZonesPage() {
  const { infrastructure } = useApp()
  const { showToast } = useToast()
  const { siteId, sites } = useSiteScope()
  const [state, setState] = useState<ZonesState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [editor, setEditor] = useState<Zone | 'create' | null>(null)
  const [deleting, setDeleting] = useState<Zone | null>(null)
  // One action list for the table and the cards.
  const rowActions = (zone: Zone): ResourceRowAction[] => [
    { kind: 'edit', label: `Edit ${zone.spec.name}`, onClick: () => setEditor(zone) },
    { kind: 'delete', label: `Delete ${zone.spec.name}`, onClick: () => setDeleting(zone) },
  ]

  // Site names are resolved from the global scope registry so a row shows a human label rather
  // than the opaque site id the Zone carries.
  const siteName = useCallback(
    (id: string) => sites.find((site) => site.id === id)?.name ?? id,
    [sites],
  )

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const zones = await infrastructure.listZones(siteId)
      setState({ status: 'ready', zones })
    } catch (caught) {
      setState({ status: 'error', ...failureOf(caught, 'Could not load zones') })
    }
  }, [infrastructure, siteId])

  useEffect(() => {
    let cancelled = false
    infrastructure
      .listZones(siteId)
      .then((zones) => {
        if (!cancelled) setState({ status: 'ready', zones })
      })
      .catch((caught: Error) => {
        if (!cancelled) setState({ status: 'error', ...failureOf(caught, 'Could not load zones') })
      })
    return () => {
      cancelled = true
    }
  }, [infrastructure, siteId])

  const settling =
    state.status === 'ready' &&
    state.zones.some((zone) => isSettling(describeConvergence(zone.status.conditions, zone.generation).state))
  const refresh = useCallback(async () => {
    const zones = await infrastructure.listZones(siteId)
    setState({ status: 'ready', zones })
  }, [infrastructure, siteId])
  useSettlingRefresh(settling, refresh)

  const visibleZones = useMemo(() => {
    if (state.status !== 'ready') return []
    const needle = query.trim().toLowerCase()
    if (!needle) return state.zones
    return state.zones.filter((zone) =>
      [zone.spec.name, zone.spec.description, siteName(zone.siteId)].some((value) => value.toLowerCase().includes(needle)),
    )
  }, [query, siteName, state])

  const handleSaved = async () => {
    const created = editor === 'create'
    setEditor(null)
    await load()
    showToast({ tone: 'success', title: created ? 'Zone created' : 'Zone updated' })
  }

  const handleDeleted = async () => {
    setDeleting(null)
    await load()
    showToast({ tone: 'success', title: 'Zone deleted' })
  }

  return (
    <div className="console-page">
      <InfrastructureHeader
        actions={
          <Button colorPalette="brand" onClick={() => setEditor('create')}>
            <Plus size={16} />
            Create zone
          </Button>
        }
      />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder="Search zones" aria-label="Search zones" />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={5} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => void load()} />}
      {state.status === 'ready' && visibleZones.length === 0 && (
        <EmptyState
          title="No zones"
          message={query ? 'No results match this search.' : `Create a zone to group servers${siteId ? ' in this site' : ''}.`}
          action={!query ? { label: 'Create zone', onClick: () => setEditor('create') } : undefined}
        />
      )}
      {state.status === 'ready' && visibleZones.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Zones">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Site</Table.ColumnHeader>
                    <Table.ColumnHeader>Description</Table.ColumnHeader>
                    <Table.ColumnHeader>Provisioner sync</Table.ColumnHeader>
                    <Table.ColumnHeader>Updated</Table.ColumnHeader>
                    <Table.ColumnHeader>
                      <VisuallyHidden>Actions</VisuallyHidden>
                    </Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleZones.map((zone) => (
                    <Table.Row key={zone.id} id={`zone-${zone.id}`}>
                      <Table.Cell>
                        <strong>{zone.spec.name}</strong>
                        <Text as="small" display="block" color="fg.muted" className="sw-mono">
                          {zone.id}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>{siteName(zone.siteId)}</Table.Cell>
                      <Table.Cell>{zone.spec.description || '-'}</Table.Cell>
                      <Table.Cell>
                        <ConvergenceStatus conditions={zone.status.conditions} generation={zone.generation} />
                      </Table.Cell>
                      <Table.Cell>{formatDateTime(zone.status.updatedAt)}</Table.Cell>
                      <Table.Cell textAlign="end">
                        <ResourceRowActions actions={rowActions(zone)} />
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Zones">
              {visibleZones.map((zone) => (
                <ResourceCard
                  key={zone.id}
                  title={zone.spec.name}
                  description={zone.spec.description || zone.id}
                  actions={
                    <ResourceRowActions actions={rowActions(zone)} />
                  }
                >
                  <ResourceCardField label="Site">{siteName(zone.siteId)}</ResourceCardField>
                  <ResourceCardField label="Provisioner sync">
                    <ConvergenceStatus conditions={zone.status.conditions} generation={zone.generation} />
                  </ResourceCardField>
                  <ResourceCardField label="Updated">{formatDateTime(zone.status.updatedAt)}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}

      {editor && (
        <ZoneDialog
          zone={editor === 'create' ? undefined : editor}
          sites={sites}
          defaultSiteId={siteId}
          onClose={() => setEditor(null)}
          onSaved={() => void handleSaved()}
        />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel="Zone"
          name={deleting.spec.name}
          intro="Removing this zone deletes it from Swallow and, when the site's provisioner supports grouping, from the provisioner too."
          warning="The provisioner may refuse if the zone still has servers."
          onClose={() => setDeleting(null)}
          onDelete={() => infrastructure.deleteZone(deleting.id)}
          onDeleted={() => void handleDeleted()}
        />
      )}
    </div>
  )
}
