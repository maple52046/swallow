import { useState } from 'react'
import { Badge, Button, HStack, Stack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { UserPlus } from 'lucide-react'
import { useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { PoolAccessPolicy, PoolMember, SudoSetting } from '@/domain/access/accounts'
import { canOperatePool } from '@/domain/user/access'
import type { PoolRole } from '@/domain/user/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Select } from '@/presentation/components/ui/select'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { AddMemberDialog } from './AddMemberDialog'
import { PoolSudoDefaultsSection } from './PoolSudoDefaultsSection'
import {
  effectiveLabel,
  POOL_ROLE_OPTIONS,
  poolRoleLabel,
  sudoSettingOptions,
  toPoolRole,
  toSudoSetting,
} from './poolAccessPresentation'
import { RemoveMemberDialog } from './RemoveMemberDialog'

/** Everything the page shows about one Pool, read together so the page renders once. */
interface PoolAccess {
  name: string
  policy: PoolAccessPolicy
  members: PoolMember[]
}

/** Which dialog is open; at most one at a time. */
type DialogState = { kind: 'add' } | { kind: 'remove'; member: PoolMember } | null

/** One membership change: what a row's selector asks to change. */
type MemberChange = { role?: PoolRole; sudo?: SudoSetting; sudoPasswordless?: SudoSetting }

/**
 * One Pool's members and Sudo Policy (route `/pools/:id`, contract pool-access.md, decision 082).
 *
 * Readable by admins and by every member of the Pool; a user outside it gets `not_found`, which the
 * page shows as "not found" without revealing whether the Pool exists. Admins and the Pool's owners
 * see editable selectors and the add and remove actions; members see the same values read-only. A
 * row's selector writes at once and the page re-reads the members, so each effective value always
 * comes from the API, never from a local guess. Changes reach the Servers within a minute through
 * the Directory, which the page says rather than waits for.
 */
export function PoolAccessPage() {
  const { id = '' } = useParams()
  const { infrastructure, poolAccess } = useApp()
  const { currentUser, restoreSession } = useAuth()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [dialog, setDialog] = useState<DialogState>(null)
  const [savingUserId, setSavingUserId] = useState<string | null>(null)
  const canManage = canOperatePool(currentUser, id)

  const access = useAsyncData<PoolAccess>(async () => {
    const [policy, members, pools] = await Promise.all([
      poolAccess.getPolicy(id),
      poolAccess.listMembers(id),
      infrastructure.listPools(),
    ])
    const name = pools.find((pool) => pool.id === id)?.name ?? id
    return { name, policy, members }
  }, [poolAccess, infrastructure, id], { unavailableStatuses: [404] })

  const changeMember = async (member: PoolMember, change: MemberChange) => {
    setSavingUserId(member.userId)
    try {
      await poolAccess.updateMember(id, member.userId, change)
      showToast({ tone: 'success', title: `${member.username} updated` })
      access.reload()
      // The caller's own role decides what the console offers; re-read it after changing it.
      if (member.userId === currentUser?.id) restoreSession()
    } catch (caught) {
      showToast({ tone: 'error', title: `${member.username} not updated`, description: failureText(failureOf(caught, 'The membership could not be changed.')) })
    } finally {
      setSavingUserId(null)
    }
  }

  const finishDialog = (title: string) => {
    const removedSelf = dialog?.kind === 'remove' && dialog.member.userId === currentUser?.id
    setDialog(null)
    showToast({ tone: 'success', title })
    access.reload()
    if (removedSelf) restoreSession()
  }

  const title = access.status === 'ready' ? access.data.name : 'Pool'
  const addAction = canManage ? (
    <Button colorPalette="brand" onClick={() => setDialog({ kind: 'add' })} disabled={access.status !== 'ready'}>
      <UserPlus size={16} />
      Add member
    </Button>
  ) : undefined

  return (
    <div className="console-page">
      <PageHeader
        title={title}
        breadcrumbs={[{ label: 'Pools', href: scopedHref('/pools') }, { label: title }]}
        subtitle="Members log in to this Pool's Servers with their SSH keys. Owners operate the Pool and decide who has sudo."
        actions={addAction}
      />
      {access.status === 'loading' && <LoadingState rows={4} />}
      {access.status === 'unavailable' && (
        <EmptyState title="Pool not found" message="The Pool does not exist, or you are not one of its members." />
      )}
      {access.status === 'error' && <ErrorState message={access.message} requestId={access.requestId} onRetry={access.reload} />}
      {access.status === 'ready' && (
        <Stack gap="5">
          <PoolSudoDefaultsSection
            key={`${access.data.policy.memberSudo}-${access.data.policy.sudoPasswordless}-${access.data.policy.updatedAt ?? ''}`}
            poolId={id}
            policy={access.data.policy}
            canManage={canManage}
            onSaved={access.reload}
          />
          <SectionSurface
            title="Members"
            description="Changes reach the Servers within a minute. A Platform may narrow what members can do on its nodes; on a Slurm cluster they log in only to login nodes, without sudo."
            flush={access.data.members.length > 0}
          >
            {access.data.members.length === 0 ? (
              <EmptyState title="No members" message={canManage ? 'Add the people who should use this Pool.' : 'This Pool has no members yet.'} />
            ) : (
              <MemberList
                members={access.data.members}
                policy={access.data.policy}
                canManage={canManage}
                currentUserId={currentUser?.id ?? ''}
                savingUserId={savingUserId}
                onChange={(member, change) => void changeMember(member, change)}
                onRemove={(member) => setDialog({ kind: 'remove', member })}
              />
            )}
          </SectionSurface>
        </Stack>
      )}
      {dialog?.kind === 'add' && access.status === 'ready' && (
        <AddMemberDialog poolId={id} poolName={access.data.name} policy={access.data.policy} onClose={() => setDialog(null)} onAdded={finishDialog} />
      )}
      {dialog?.kind === 'remove' && access.status === 'ready' && (
        <RemoveMemberDialog
          poolId={id}
          poolName={access.data.name}
          member={dialog.member}
          isSelf={dialog.member.userId === currentUser?.id}
          onClose={() => setDialog(null)}
          onRemoved={finishDialog}
        />
      )}
    </div>
  )
}

interface MemberListProps {
  members: PoolMember[]
  policy: PoolAccessPolicy
  /** Whether the caller is an admin or an owner of the Pool, which makes the rows editable. */
  canManage: boolean
  currentUserId: string
  /** The member whose change is in flight; its selectors are disabled until the write returns. */
  savingUserId: string | null
  onChange: (member: PoolMember, change: MemberChange) => void
  onRemove: (member: PoolMember) => void
}

/**
 * The memberships as a table on desktop and cards on mobile, built from the same cells. Each sudo
 * value shows its effective result and why in words; an owner's sudo selector is disabled because an
 * owner always has sudo, whatever the override says.
 */
function MemberList({ members, policy, canManage, currentUserId, savingUserId, onChange, onRemove }: MemberListProps) {
  const roleCell = (member: PoolMember) => canManage ? (
    <Select
      size="sm"
      aria-label={`Role of ${member.username}`}
      value={member.role}
      onChange={(value) => onChange(member, { role: toPoolRole(value) })}
      options={POOL_ROLE_OPTIONS}
      disabled={savingUserId === member.userId}
    />
  ) : (
    <Badge colorPalette={member.role === 'owner' ? 'blue' : 'gray'} variant="subtle">{poolRoleLabel(member.role)}</Badge>
  )

  const sudoCell = (member: PoolMember) => (
    <Stack gap="1">
      {canManage && member.role === 'member' && (
        <Select
          size="sm"
          aria-label={`Sudo of ${member.username}`}
          value={member.sudo.setting}
          onChange={(value) => onChange(member, { sudo: toSudoSetting(value) })}
          options={sudoSettingOptions(policy.memberSudo)}
          disabled={savingUserId === member.userId}
        />
      )}
      <Text fontSize="xs" color="fg.muted">{effectiveLabel(member.sudo)}</Text>
    </Stack>
  )

  const passwordlessCell = (member: PoolMember) => (
    <Stack gap="1">
      {canManage && (
        <Select
          size="sm"
          aria-label={`Sudo without password for ${member.username}`}
          value={member.sudoPasswordless.setting}
          onChange={(value) => onChange(member, { sudoPasswordless: toSudoSetting(value) })}
          options={sudoSettingOptions(policy.sudoPasswordless)}
          disabled={savingUserId === member.userId}
        />
      )}
      <Text fontSize="xs" color="fg.muted">{effectiveLabel(member.sudoPasswordless)}</Text>
    </Stack>
  )

  const name = (member: PoolMember) => (
    <HStack gap="2">
      <Text as="span" fontWeight="semibold" className="sw-mono">{member.username}</Text>
      {member.userId === currentUserId && <Badge variant="outline">you</Badge>}
    </HStack>
  )

  const remove = (member: PoolMember) => canManage ? (
    <Button size="xs" variant="outline" colorPalette="red" onClick={() => onRemove(member)} aria-label={`Remove ${member.username}`}>
      Remove
    </Button>
  ) : null

  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Pool members">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Username</Table.ColumnHeader>
                <Table.ColumnHeader minW="9rem">Role</Table.ColumnHeader>
                <Table.ColumnHeader minW="12rem">Sudo</Table.ColumnHeader>
                <Table.ColumnHeader minW="12rem">Sudo without password</Table.ColumnHeader>
                {canManage && (
                  <Table.ColumnHeader>
                    <VisuallyHidden>Actions</VisuallyHidden>
                  </Table.ColumnHeader>
                )}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {members.map((member) => (
                <Table.Row key={member.userId}>
                  <Table.Cell>{name(member)}</Table.Cell>
                  <Table.Cell>{roleCell(member)}</Table.Cell>
                  <Table.Cell>{sudoCell(member)}</Table.Cell>
                  <Table.Cell>{passwordlessCell(member)}</Table.Cell>
                  {canManage && <Table.Cell textAlign="end">{remove(member)}</Table.Cell>}
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label="Pool members">
          {members.map((member) => (
            <ResourceCard key={member.userId} title={member.username} actions={remove(member)}>
              <ResourceCardField label="Role">{roleCell(member)}</ResourceCardField>
              <ResourceCardField label="Sudo">{sudoCell(member)}</ResourceCardField>
              <ResourceCardField label="Sudo without password">{passwordlessCell(member)}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
