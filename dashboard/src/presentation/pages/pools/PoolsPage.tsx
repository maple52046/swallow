import { useMemo } from 'react'
import { Badge, Button, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isAdmin } from '@/domain/user/access'
import type { PoolRole, User } from '@/domain/user/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { poolRoleLabel } from './poolAccessPresentation'

/** One Pool as this page lists it; `role` is `null` for an admin who is not a member. */
interface PoolRow {
  poolId: string
  name: string
  siteId: string
  role: PoolRole | null
}

/**
 * The signed-in user's memberships as rows, from auth-me; the Site scope narrows them like every
 * Site-scoped list. No request is made: auth-me already carries them.
 */
function membershipRows(user: User | null, siteId: string | undefined): PoolRow[] {
  return (user?.memberships ?? [])
    .filter((membership) => !siteId || membership.siteId === siteId)
    .map((membership) => ({ poolId: membership.poolId, name: membership.poolName, siteId: membership.siteId, role: membership.role }))
}

/**
 * The Pools the signed-in user can manage access for (route `/pools`, decision 082).
 *
 * An admin sees every Pool of the Site scope, read from the infrastructure contract, with its own
 * membership where it has one; any other user sees only its own memberships, which auth-me already
 * reported (it may not list other Pools' members, so nothing more would open). Each row opens the
 * Pool's members and sudo defaults. The list is read once per visit.
 */
export function PoolsPage() {
  const { infrastructure } = useApp()
  const { currentUser } = useAuth()
  const { siteId, sites, scopedHref } = useSiteScope()
  const admin = isAdmin(currentUser)
  const pools = useAsyncData(
    () => (admin ? infrastructure.listGroups('pool', siteId) : Promise.resolve([])),
    [infrastructure, admin, siteId],
    { unavailableStatuses: [] },
  )
  const siteNames = useMemo(() => new Map(sites.map((site) => [site.id, site.name])), [sites])

  const rows = useMemo<PoolRow[] | null>(() => {
    if (!admin) return membershipRows(currentUser, siteId)
    if (pools.status !== 'ready') return null
    const memberships = new Map((currentUser?.memberships ?? []).map((membership) => [membership.poolId, membership.role]))
    return pools.data.map((pool) => ({ poolId: pool.id, name: pool.spec.name, siteId: pool.siteId, role: memberships.get(pool.id) ?? null }))
  }, [admin, currentUser, pools, siteId])

  return (
    <div className="console-page">
      <PageHeader
        title="Pools"
        subtitle="Who may use each Pool's Servers, and whether they have sudo there."
      />
      {admin && pools.status === 'loading' && <LoadingState rows={3} />}
      {admin && (pools.status === 'error' || pools.status === 'unavailable') && (
        <ErrorState message={pools.message} requestId={pools.status === 'error' ? pools.requestId : undefined} onRetry={pools.reload} />
      )}
      {rows && (
        <SectionSurface
          title={admin ? 'Every Pool' : 'Your Pools'}
          description={admin ? 'Create and delete Pools under Infrastructure; manage who uses them here.' : 'The Pools an owner added you to.'}
          flush={rows.length > 0}
        >
          {rows.length === 0 ? (
            <EmptyState
              title="No Pools"
              message={admin ? 'Create a Pool under Infrastructure, then add its members here.' : 'Ask a Pool owner or an admin to add you to a Pool.'}
            />
          ) : (
            <PoolList rows={rows} siteNames={siteNames} hrefFor={(row) => scopedHref(`/pools/${encodeURIComponent(row.poolId)}`, row.siteId)} />
          )}
        </SectionSurface>
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
  siteNames: ReadonlyMap<string, string>
  hrefFor: (row: PoolRow) => string
}

/** The Pools as a table on desktop and cards on mobile; each links to its access page. */
function PoolList({ rows, siteNames, hrefFor }: PoolListProps) {
  const open = (row: PoolRow) => (
    <Button asChild size="xs" variant="outline">
      <RouterLink to={hrefFor(row)} aria-label={`Members of ${row.name}`}>Members</RouterLink>
    </Button>
  )
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Pools">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Pool</Table.ColumnHeader>
                <Table.ColumnHeader>Site</Table.ColumnHeader>
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
                  <Table.Cell>{siteNames.get(row.siteId) ?? row.siteId}</Table.Cell>
                  <Table.Cell><RoleValue role={row.role} /></Table.Cell>
                  <Table.Cell textAlign="end">{open(row)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label="Pools">
          {rows.map((row) => (
            <ResourceCard key={row.poolId} title={row.name} description={siteNames.get(row.siteId) ?? row.siteId} actions={open(row)}>
              <ResourceCardField label="Your role"><RoleValue role={row.role} /></ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
