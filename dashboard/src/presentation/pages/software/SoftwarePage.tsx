import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Button, HStack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Plus, Settings } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import type { SoftwareAssignment, SoftwareCatalogEntry } from '@/domain/software/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { InstallSoftwareDialog } from './InstallSoftwareDialog'
import { UninstallSoftwareDialog } from './UninstallSoftwareDialog'
import { assignmentStateLabel, assignmentStatePalette, softwareKindLabel } from './softwarePresentation'

/** The data the page needs once loaded: the catalog, current assignments, and deployed Servers. */
interface SoftwareData {
  catalog: SoftwareCatalogEntry[]
  assignments: SoftwareAssignment[]
  /** Deployed Servers in the active Site, used for the id->name map and the install picker. */
  servers: Server[]
}

type SoftwareState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: SoftwareData }

/**
 * Managed Software workspace (decision 038): install a single piece of host software on deployed
 * Servers, list the swallow-owned Software Assignments, and uninstall one from a Server. Results
 * honor the global Site scope: assignments are cross-referenced against the Site's deployed Servers
 * so only in-scope records are shown, and only in-scope deployed Servers are offered as install
 * targets. Install and uninstall create Workflows; on acceptance the page navigates to the
 * Workflow's progress view so the operator follows the same per-Task Job timeline as every other
 * deployment. Settings that belong to one software kind (such as Docker CE's Registry credentials)
 * live on the Settings page this one links to, never on this software-wide list.
 */
export function SoftwarePage() {
  const { software, servers } = useApp()
  const { siteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const [state, setState] = useState<SoftwareState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [installing, setInstalling] = useState(false)
  const [uninstalling, setUninstalling] = useState<SoftwareAssignment | null>(null)

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const [catalog, assignments, workingSet] = await Promise.all([
        software.listCatalog(),
        software.listAssignments(),
        loadServerWorkingSet(servers, { siteId, provisioningState: 'deployed' }),
      ])
      setState({ status: 'ready', data: { catalog, assignments, servers: workingSet.servers } })
    } catch (caught) {
      setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load software.' })
    }
  }, [software, servers, siteId])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const [catalog, assignments, workingSet] = await Promise.all([
          software.listCatalog(),
          software.listAssignments(),
          loadServerWorkingSet(servers, { siteId, provisioningState: 'deployed' }),
        ])
        if (!cancelled) {
          setState({ status: 'ready', data: { catalog, assignments, servers: workingSet.servers } })
        }
      } catch (caught) {
        if (!cancelled) {
          setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load software.' })
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [software, servers, siteId])

  // Resolve a Server id to its display label, and remember which Servers are in the active Site so
  // assignments for out-of-scope Servers are hidden along with the Site filter.
  const serverIndex = useMemo(() => {
    const names = new Map<string, string>()
    if (state.status === 'ready') {
      for (const server of state.data.servers) names.set(server.id, serverDisplayName(server))
    }
    return names
  }, [state])

  const catalogLabels = useMemo(() => {
    const labels = new Map<string, string>()
    if (state.status === 'ready') {
      for (const entry of state.data.catalog) labels.set(entry.kind, entry.label)
    }
    return labels
  }, [state])

  const visibleAssignments = useMemo(() => {
    if (state.status !== 'ready') return []
    const needle = query.trim().toLowerCase()
    return state.data.assignments
      // Scope to the active Site by keeping only assignments whose Server is in the Site's set.
      // When no Site is selected the whole fleet's deployed Servers are loaded, so nothing is lost.
      .filter((assignment) => serverIndex.has(assignment.serverId))
      .filter((assignment) => {
        if (!needle) return true
        const label = softwareKindLabel(assignment.kind, catalogLabels.get(assignment.kind))
        const serverName = serverIndex.get(assignment.serverId) ?? assignment.serverId
        return [label, serverName, assignment.serverId, assignment.state].some((value) =>
          value.toLowerCase().includes(needle),
        )
      })
  }, [state, query, serverIndex, catalogLabels])

  const serverLabel = useCallback(
    (serverId: string) => serverIndex.get(serverId) ?? serverId,
    [serverIndex],
  )

  const handleLaunched = (operationId: string, toneTitle: string) => {
    setInstalling(false)
    setUninstalling(null)
    showToast({ tone: 'success', title: toneTitle })
    navigate(scopedHref(`/workflows/${operationId}`))
  }

  const renderRoles = (assignment: SoftwareAssignment) =>
    assignment.roles.length > 0 ? assignment.roles.join(', ') : '-'

  const renderState = (assignment: SoftwareAssignment) => (
    <Badge colorPalette={assignmentStatePalette(assignment.state)} variant="subtle">
      {assignmentStateLabel(assignment.state)}
    </Badge>
  )

  return (
    <div className="operator-page">
      <SectionHeader
        title="Software"
        description="Install a single piece of host software (Docker CE, Podman, NFS) on deployed Servers, separate from platform deployment."
        actions={
          <HStack gap="2">
            <Button variant="outline" onClick={() => navigate(scopedHref('/software/settings'))}>
              <Settings size={16} aria-hidden />
              Settings
            </Button>
            <Button colorPalette="brand" onClick={() => setInstalling(true)} disabled={state.status !== 'ready'}>
              <Plus size={16} />
              Install software
            </Button>
          </HStack>
        }
      />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder="Search software" aria-label="Search software" />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={5} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {state.status === 'ready' && visibleAssignments.length === 0 && (
        <EmptyState
          title="No software installed"
          message={
            query
              ? 'No results match this search.'
              : 'Install software on one or more deployed Servers to see it here.'
          }
          action={!query ? { label: 'Install software', onClick: () => setInstalling(true) } : undefined}
        />
      )}
      {state.status === 'ready' && visibleAssignments.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Software assignments">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Server</Table.ColumnHeader>
                    <Table.ColumnHeader>Software</Table.ColumnHeader>
                    <Table.ColumnHeader>Roles</Table.ColumnHeader>
                    <Table.ColumnHeader>State</Table.ColumnHeader>
                    <Table.ColumnHeader>Last applied</Table.ColumnHeader>
                    <Table.ColumnHeader>
                      <VisuallyHidden>Actions</VisuallyHidden>
                    </Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleAssignments.map((assignment) => (
                    <Table.Row key={`${assignment.serverId}/${assignment.kind}`}>
                      <Table.Cell>
                        <strong>{serverLabel(assignment.serverId)}</strong>
                        <Text as="small" display="block" color="fg.muted" className="sw-mono">
                          {assignment.serverId}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>{softwareKindLabel(assignment.kind, catalogLabels.get(assignment.kind))}</Table.Cell>
                      <Table.Cell>{renderRoles(assignment)}</Table.Cell>
                      <Table.Cell>{renderState(assignment)}</Table.Cell>
                      <Table.Cell>{assignment.lastAppliedAt ? formatDateTime(assignment.lastAppliedAt) : '-'}</Table.Cell>
                      <Table.Cell textAlign="end">
                        <Button
                          size="xs"
                          variant="outline"
                          colorPalette="red"
                          onClick={() => setUninstalling(assignment)}
                        >
                          Uninstall
                        </Button>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Software assignments">
              {visibleAssignments.map((assignment) => (
                <ResourceCard
                  key={`${assignment.serverId}/${assignment.kind}`}
                  title={serverLabel(assignment.serverId)}
                  description={assignment.serverId}
                  status={renderState(assignment)}
                  actions={
                    <Button
                      size="xs"
                      variant="outline"
                      colorPalette="red"
                      onClick={() => setUninstalling(assignment)}
                    >
                      Uninstall
                    </Button>
                  }
                >
                  <ResourceCardField label="Software">
                    {softwareKindLabel(assignment.kind, catalogLabels.get(assignment.kind))}
                  </ResourceCardField>
                  <ResourceCardField label="Roles">{renderRoles(assignment)}</ResourceCardField>
                  <ResourceCardField label="Last applied">
                    {assignment.lastAppliedAt ? formatDateTime(assignment.lastAppliedAt) : '-'}
                  </ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}

      {installing && state.status === 'ready' && (
        <InstallSoftwareDialog
          catalog={state.data.catalog}
          servers={state.data.servers}
          onClose={() => setInstalling(false)}
          onLaunched={(operationId) => handleLaunched(operationId, 'Software install started')}
        />
      )}
      {uninstalling && (
        <UninstallSoftwareDialog
          assignment={uninstalling}
          serverLabel={serverLabel(uninstalling.serverId)}
          onClose={() => setUninstalling(null)}
          onLaunched={(operationId) => handleLaunched(operationId, 'Software uninstall started')}
        />
      )}
    </div>
  )
}
