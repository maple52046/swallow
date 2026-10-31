import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { type GroupKind, type GroupingResource, groupKindLabel } from '@/domain/infrastructure/types'
import { ConvergenceStatus } from '@/presentation/components/convergence/ConvergenceStatus'
import { describeConvergence, isSettling } from '@/presentation/components/convergence/convergencePresentation'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { ResourceRowActions } from '@/presentation/components/ResourceRowActions'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { GroupDialog } from './GroupDialog'
import { InfrastructureHeader } from './InfrastructureHeader'
import { ResourceDeleteDialog } from './ResourceDeleteDialog'

interface GroupingPageProps {
  /** Whether this screen manages Zones or Pools. One component serves both. */
  kind: GroupKind
}

type GroupingState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; groups: GroupingResource[] }

/** How often the list is re-read while a saved change is still being applied to provisioners. */
const SETTLE_POLL_MS = 5000

/**
 * Admin registry for swallow-owned Zones or Pools, sharing one implementation across both
 * kinds (decision 071). Results honor the global Site scope, and each row shows whether the Site's
 * provisioners hold the group as specified. Desktop and mobile share the same filtered working set
 * and mutation callbacks; a save or delete refreshes the list, and while any group is still being
 * applied the list is re-read quietly so its status settles in place (a failed re-read keeps the
 * last list).
 */
export function GroupingPage({ kind }: GroupingPageProps) {
  const { infrastructure } = useApp()
  const { showToast } = useToast()
  const { siteId, sites } = useSiteScope()
  const [state, setState] = useState<GroupingState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [editor, setEditor] = useState<GroupingResource | 'create' | null>(null)
  const [deleting, setDeleting] = useState<GroupingResource | null>(null)

  const label = groupKindLabel(kind)
  const noun = label.toLowerCase()
  // Site names are resolved from the global scope registry so a row shows a human label rather
  // than the opaque site id the group carries.
  const siteName = useCallback(
    (id: string) => sites.find((site) => site.id === id)?.name ?? id,
    [sites],
  )

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const groups = await infrastructure.listGroups(kind, siteId)
      setState({ status: 'ready', groups })
    } catch (caught) {
      setState({ status: 'error', message: caught instanceof Error ? caught.message : `Could not load ${noun}s` })
    }
  }, [infrastructure, kind, noun, siteId])

  useEffect(() => {
    let cancelled = false
    infrastructure
      .listGroups(kind, siteId)
      .then((groups) => {
        if (!cancelled) setState({ status: 'ready', groups })
      })
      .catch((caught: Error) => {
        if (!cancelled) setState({ status: 'error', message: caught.message })
      })
    return () => {
      cancelled = true
    }
  }, [infrastructure, kind, siteId])

  const settling =
    state.status === 'ready' &&
    state.groups.some((group) => isSettling(describeConvergence(group.status.conditions, group.generation).state))
  useEffect(() => {
    if (!settling) return
    let cancelled = false
    const timer = window.setTimeout(() => {
      infrastructure
        .listGroups(kind, siteId)
        .then((groups) => {
          if (!cancelled) setState({ status: 'ready', groups })
        })
        .catch(() => undefined)
    }, SETTLE_POLL_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [settling, state, infrastructure, kind, siteId])

  const visibleGroups = useMemo(() => {
    if (state.status !== 'ready') return []
    const needle = query.trim().toLowerCase()
    if (!needle) return state.groups
    return state.groups.filter((group) =>
      [group.spec.name, group.spec.description, siteName(group.siteId)].some((value) => value.toLowerCase().includes(needle)),
    )
  }, [query, siteName, state])

  const handleSaved = async () => {
    const created = editor === 'create'
    setEditor(null)
    await load()
    showToast({ tone: 'success', title: created ? `${label} created` : `${label} updated` })
  }

  const handleDeleted = async () => {
    setDeleting(null)
    await load()
    showToast({ tone: 'success', title: `${label} deleted` })
  }

  return (
    <div className="operator-page">
      <InfrastructureHeader
        actions={
          <Button colorPalette="brand" onClick={() => setEditor('create')}>
            <Plus size={16} />
            Create {noun}
          </Button>
        }
      />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder={`Search ${noun}s`} aria-label={`Search ${noun}s`} />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={5} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {state.status === 'ready' && visibleGroups.length === 0 && (
        <EmptyState
          title={`No ${noun}s`}
          message={
            query
              ? 'No results match this search.'
              : `Create a ${noun} to group servers${siteId ? ' in this site' : ''}.`
          }
          action={!query ? { label: `Create ${noun}`, onClick: () => setEditor('create') } : undefined}
        />
      )}
      {state.status === 'ready' && visibleGroups.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label={`${label}s`}>
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
                  {visibleGroups.map((group) => (
                    <Table.Row key={group.id} id={`${kind}-${group.id}`}>
                      <Table.Cell>
                        <strong>{group.spec.name}</strong>
                        <Text as="small" display="block" color="fg.muted" className="sw-mono">
                          {group.id}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>{siteName(group.siteId)}</Table.Cell>
                      <Table.Cell>{group.spec.description || '-'}</Table.Cell>
                      <Table.Cell>
                        <ConvergenceStatus conditions={group.status.conditions} generation={group.generation} />
                      </Table.Cell>
                      <Table.Cell>{formatDateTime(group.status.updatedAt)}</Table.Cell>
                      <Table.Cell textAlign="end">
                        <ResourceRowActions
                          actions={[
                            { kind: 'edit', label: `Edit ${group.spec.name}`, onClick: () => setEditor(group) },
                            { kind: 'delete', label: `Delete ${group.spec.name}`, onClick: () => setDeleting(group) },
                          ]}
                        />
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label={`${label}s`}>
              {visibleGroups.map((group) => (
                <ResourceCard
                  key={group.id}
                  title={group.spec.name}
                  description={group.spec.description || group.id}
                  actions={
                    <ResourceRowActions
                      actions={[
                        { kind: 'edit', label: `Edit ${group.spec.name}`, onClick: () => setEditor(group) },
                        { kind: 'delete', label: `Delete ${group.spec.name}`, onClick: () => setDeleting(group) },
                      ]}
                    />
                  }
                >
                  <ResourceCardField label="Site">{siteName(group.siteId)}</ResourceCardField>
                  <ResourceCardField label="Provisioner sync">
                    <ConvergenceStatus conditions={group.status.conditions} generation={group.generation} />
                  </ResourceCardField>
                  <ResourceCardField label="Updated">{formatDateTime(group.status.updatedAt)}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}

      {editor && (
        <GroupDialog
          kind={kind}
          group={editor === 'create' ? undefined : editor}
          sites={sites}
          defaultSiteId={siteId}
          onClose={() => setEditor(null)}
          onSaved={() => void handleSaved()}
        />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel={label}
          name={deleting.spec.name}
          intro={`Removing this ${noun} deletes it from Swallow and, when the site's provisioner supports grouping, from the provisioner too.`}
          warning={`The provisioner may refuse if the ${noun} still has servers.`}
          onClose={() => setDeleting(null)}
          onDelete={() => infrastructure.deleteGroup(kind, deleting.id)}
          onDeleted={() => void handleDeleted()}
        />
      )}
    </div>
  )
}
