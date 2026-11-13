import { useMemo, useState } from 'react'
import { Badge, Button, HStack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Pool } from '@/domain/infrastructure/types'
import { isAdmin } from '@/domain/user/access'
import type { PoolRole, User } from '@/domain/user/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { ResourceRowActions, type ResourceRowAction } from '@/presentation/components/ResourceRowActions'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { ResourceDeleteDialog } from '@/presentation/pages/infrastructure/ResourceDeleteDialog'
import { PoolDialog } from './PoolDialog'
import { poolRoleLabel } from './poolAccessPresentation'

/**
 * One Pool as this page lists it; `role` is `null` for an admin who is not a member, and `pool` is
 * the full record an admin can edit (a non-admin row comes from auth-me and has none).
 */
interface PoolRow {
  poolId: string
  name: string
  description: string
  role: PoolRole | null
  pool: Pool | null
}

/** The signed-in user's memberships as rows, from auth-me; no request is made. */
function membershipRows(user: User | null): PoolRow[] {
  return (user?.memberships ?? []).map((membership) => ({
    poolId: membership.poolId,
    name: membership.poolName,
    description: '',
    role: membership.role,
    pool: null,
  }))
}

/**
 * The Pools of the installation (route `/pools`, decision 082).
 *
 * A Pool belongs to no Site, so the list ignores the Site scope. An admin sees every Pool, read
 * from the infrastructure contract, with its own membership where it has one, and creates, renames,
 * and deletes Pools here; any other user sees only its own memberships, which auth-me already
 * reported (it may not list other Pools' members, so nothing more would open). Each row opens the
 * Pool's members and sudo defaults. The list is read once per visit and again after an admin's
 * change.
 */
export function PoolsPage() {
  const { infrastructure } = useApp()
  const { currentUser } = useAuth()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const admin = isAdmin(currentUser)
  const [editor, setEditor] = useState<Pool | 'create' | null>(null)
  const [deleting, setDeleting] = useState<Pool | null>(null)
  const pools = useAsyncData(
    () => (admin ? infrastructure.listPools() : Promise.resolve([])),
    [infrastructure, admin],
    { unavailableStatuses: [] },
  )

  const rows = useMemo<PoolRow[] | null>(() => {
    if (!admin) return membershipRows(currentUser)
    if (pools.status !== 'ready') return null
    const memberships = new Map((currentUser?.memberships ?? []).map((membership) => [membership.poolId, membership.role]))
    return pools.data.map((pool) => ({
      poolId: pool.id, name: pool.name, description: pool.description, role: memberships.get(pool.id) ?? null, pool,
    }))
  }, [admin, currentUser, pools])

  const handleSaved = () => {
    const created = editor === 'create'
    setEditor(null)
    pools.reload()
    showToast({ tone: 'success', title: created ? 'Pool created' : 'Pool updated' })
  }

  const handleDeleted = () => {
    setDeleting(null)
    pools.reload()
    showToast({ tone: 'success', title: 'Pool deleted' })
  }

  // Only an admin manages the Pools themselves; everyone may open the members of the Pools listed.
  const rowActions = (row: PoolRow): ResourceRowAction[] => {
    const pool = row.pool
    if (!admin || !pool) return []
    return [
      { kind: 'edit', label: `Edit ${row.name}`, onClick: () => setEditor(pool) },
      { kind: 'delete', label: `Delete ${row.name}`, onClick: () => setDeleting(pool) },
    ]
  }

  return (
    <div className="console-page">
      <PageHeader
        title="Pools"
        subtitle="Groups of servers from any site, who may use them, and whether they have sudo there."
        actions={
          admin ? (
            <Button colorPalette="brand" onClick={() => setEditor('create')}>
              <Plus size={16} />
              Create pool
            </Button>
          ) : undefined
        }
      />
      {admin && pools.status === 'loading' && <LoadingState rows={3} />}
      {admin && (pools.status === 'error' || pools.status === 'unavailable') && (
        <ErrorState message={pools.message} requestId={pools.status === 'error' ? pools.requestId : undefined} onRetry={pools.reload} />
      )}
      {rows && (
        <SectionSurface
          title={admin ? 'Every Pool' : 'Your Pools'}
          description={admin ? 'Place servers in a Pool from the server list; manage who uses each Pool from its members.' : 'The Pools an owner added you to.'}
          flush={rows.length > 0}
        >
          {rows.length === 0 ? (
            <EmptyState
              title="No Pools"
              message={admin ? 'Create a Pool, place servers in it, then add its members.' : 'Ask a Pool owner or an admin to add you to a Pool.'}
              action={admin ? { label: 'Create pool', onClick: () => setEditor('create') } : undefined}
            />
          ) : (
            <PoolList rows={rows} hrefFor={(row) => scopedHref(`/pools/${encodeURIComponent(row.poolId)}`)} actionsFor={rowActions} />
          )}
        </SectionSurface>
      )}
      {editor && (
        <PoolDialog pool={editor === 'create' ? undefined : editor} onClose={() => setEditor(null)} onSaved={handleSaved} />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel="Pool"
          name={deleting.name}
          intro="Removing this pool deletes it from Swallow with its members and sudo defaults; nothing changes in the provisioner."
          warning="Swallow refuses while a server is still placed in the pool."
          onClose={() => setDeleting(null)}
          onDelete={() => infrastructure.deletePool(deleting.id)}
          onDeleted={handleDeleted}
        />
      )}
    </div>
  )
}

/** The caller's role in a Pool as a labelled badge; an admin who is not a member sees "Admin". */
function RoleValue({ role }: { role: PoolRole | null }) {
  if (role === null) return <Text as="span" color="fg.muted">Admin (not a member)</Text>
  return <Badge colorPalette={role === 'owner' ? 'blue' : 'gray'} variant="subtle">{poolRoleLabel(role)}</Badge>
}

interface PoolListProps {
  rows: PoolRow[]
  hrefFor: (row: PoolRow) => string
  /** The admin actions of a row (edit, delete); empty for a user who is not an admin. */
  actionsFor: (row: PoolRow) => ResourceRowAction[]
}

/**
 * The Pools as a table on desktop and cards on mobile. Each row links to its access page with a
 * named "Members" link, and carries the admin's icon actions beside it.
 */
function PoolList({ rows, hrefFor, actionsFor }: PoolListProps) {
  const actions = (row: PoolRow) => (
    <HStack gap="1" justify="flex-end">
      <Button asChild size="xs" variant="outline">
        <RouterLink to={hrefFor(row)} aria-label={`Members of ${row.name}`}>Members</RouterLink>
      </Button>
      <ResourceRowActions actions={actionsFor(row)} />
    </HStack>
  )
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Pools">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Pool</Table.ColumnHeader>
                <Table.ColumnHeader>Description</Table.ColumnHeader>
                <Table.ColumnHeader>Your role</Table.ColumnHeader>
                <Table.ColumnHeader>
                  <VisuallyHidden>Actions</VisuallyHidden>
                </Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.map((row) => (
                <Table.Row key={row.poolId}>
                  <Table.Cell fontWeight="semibold">{row.name}</Table.Cell>
                  <Table.Cell>{row.description || '-'}</Table.Cell>
                  <Table.Cell><RoleValue role={row.role} /></Table.Cell>
                  <Table.Cell textAlign="end">{actions(row)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label="Pools">
          {rows.map((row) => (
            <ResourceCard key={row.poolId} title={row.name} description={row.description || undefined} actions={actions(row)}>
              <ResourceCardField label="Your role"><RoleValue role={row.role} /></ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
