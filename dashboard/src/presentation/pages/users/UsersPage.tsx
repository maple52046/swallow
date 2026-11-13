import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, HStack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { UserPlus } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { Account } from '@/domain/access/accounts'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { formatDateTime } from '@/shared/utils/time'
import { AccessAuditSection } from './AccessAuditSection'
import { AccessSettingsSection } from './AccessSettingsSection'
import { userRoleLabel } from './accountPresentation'
import { CreateUserDialog } from './CreateUserDialog'
import { DeleteUserDialog } from './DeleteUserDialog'
import { DirectorySection } from './DirectorySection'
import { EditUserDialog } from './EditUserDialog'

/** The account list's read state; the other sections of the page load on their own. */
type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; accounts: Account[] }

/** Which dialog is open; at most one at a time. */
type DialogState = { kind: 'create' } | { kind: 'edit'; account: Account } | { kind: 'delete'; account: Account } | null

/**
 * Account administration (route `/users`, admins only through `AdminRoute`, decision 082).
 *
 * Lists every User from the users.md contract and opens the create, edit (role and password reset),
 * and delete dialogs; below it, the installation's access settings, the Directory's status, and the
 * recent access changes, each loading on its own so one failing read does not hide the others. The
 * list is read once per visit and after each change; the signed-in admin's own row offers no
 * delete, since the API refuses deleting oneself. No password is ever read from the API.
 */
export function UsersPage() {
  const { accounts } = useApp()
  const { currentUser } = useAuth()
  const { showToast } = useToast()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [dialog, setDialog] = useState<DialogState>(null)
  const [reloadNonce, setReloadNonce] = useState(0)
  // The audit list re-reads after every change made on this page, so it shows that change.
  const [auditNonce, setAuditNonce] = useState(0)

  // The `cancelled` flag drops a slower, older response so it cannot overwrite a newer one.
  useEffect(() => {
    let cancelled = false
    void accounts.listAccounts()
      .then((items) => {
        if (!cancelled) setState({ status: 'ready', accounts: items })
      })
      .catch((caught: unknown) => {
        if (!cancelled) setState({ status: 'error', ...failureOf(caught, 'Could not load users.') })
      })
    return () => {
      cancelled = true
    }
  }, [accounts, reloadNonce])

  const reload = useCallback(() => {
    setState({ status: 'loading' })
    setReloadNonce((value) => value + 1)
  }, [])

  const finishDialog = (title: string) => {
    setDialog(null)
    showToast({ tone: 'success', title })
    setReloadNonce((value) => value + 1)
    setAuditNonce((value) => value + 1)
  }

  const createAction = (
    <Button colorPalette="brand" onClick={() => setDialog({ kind: 'create' })} disabled={state.status !== 'ready'}>
      <UserPlus size={16} />
      Create user
    </Button>
  )

  return (
    <div className="console-page">
      <PageHeader
        title="Users"
        subtitle="People who sign in to Swallow. A username is also the person's Linux login on the Servers of their Pools."
        actions={createAction}
      />

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />}
      {state.status === 'ready' && (
        <SectionSurface
          title="Accounts"
          description="Admins administer Swallow and have sudo on every Server. Users reach Servers only through the Pools an owner adds them to."
          flush={state.accounts.length > 0}
        >
          {state.accounts.length === 0 ? (
            <EmptyState
              title="No users"
              message="Create a user for each person who should sign in."
              action={{ label: 'Create user', onClick: () => setDialog({ kind: 'create' }) }}
            />
          ) : (
            <AccountList
              accounts={state.accounts}
              currentUserId={currentUser?.id ?? ''}
              onEdit={(account) => setDialog({ kind: 'edit', account })}
              onDelete={(account) => setDialog({ kind: 'delete', account })}
            />
          )}
        </SectionSurface>
      )}

      <AccessSettingsSection onChanged={() => setAuditNonce((value) => value + 1)} />
      <DirectorySection />
      <AccessAuditSection reloadKey={auditNonce} />

      {dialog?.kind === 'create' && <CreateUserDialog onClose={() => setDialog(null)} onCreated={finishDialog} />}
      {dialog?.kind === 'edit' && (
        <EditUserDialog account={dialog.account} isSelf={dialog.account.id === currentUser?.id} onClose={() => setDialog(null)} onSaved={finishDialog} />
      )}
      {dialog?.kind === 'delete' && <DeleteUserDialog account={dialog.account} onClose={() => setDialog(null)} onDeleted={finishDialog} />}
    </div>
  )
}

/** The role as a labelled badge, so it is conveyed by words as well as colour. */
function RoleBadge({ account }: { account: Account }) {
  return (
    <Badge colorPalette={account.role === 'admin' ? 'purple' : 'gray'} variant="subtle">
      {userRoleLabel(account.role)}
    </Badge>
  )
}

/** The POSIX uid, or the reason there is none yet (a User from before POSIX identities). */
function uidLabel(account: Account): string {
  return account.uidNumber === null ? 'Not allocated yet' : String(account.uidNumber)
}

interface AccountListProps {
  accounts: Account[]
  /** The signed-in admin, whose row offers no delete (the API refuses deleting oneself). */
  currentUserId: string
  onEdit: (account: Account) => void
  onDelete: (account: Account) => void
}

/**
 * Every User as a table on desktop and cards on mobile, built from the same rows and callbacks. The
 * actions only open dialogs; nothing changes from here directly.
 */
function AccountList({ accounts, currentUserId, onEdit, onDelete }: AccountListProps) {
  const actions = (account: Account) => (
    <HStack gap="2" justify="flex-end">
      <Button size="xs" variant="outline" onClick={() => onEdit(account)} aria-label={`Edit ${account.username}`}>
        Edit
      </Button>
      {account.id !== currentUserId && (
        <Button size="xs" variant="outline" colorPalette="red" onClick={() => onDelete(account)} aria-label={`Delete ${account.username}`}>
          Delete
        </Button>
      )}
    </HStack>
  )
  const name = (account: Account) => (
    <HStack gap="2">
      <Text as="span" fontWeight="semibold" className="sw-mono">{account.username}</Text>
      {account.id === currentUserId && <Badge variant="outline">you</Badge>}
    </HStack>
  )

  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Users">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Username</Table.ColumnHeader>
                <Table.ColumnHeader>Role</Table.ColumnHeader>
                <Table.ColumnHeader>UID</Table.ColumnHeader>
                <Table.ColumnHeader>Created</Table.ColumnHeader>
                <Table.ColumnHeader>
                  <VisuallyHidden>Actions</VisuallyHidden>
                </Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {accounts.map((account) => (
                <Table.Row key={account.id}>
                  <Table.Cell>{name(account)}</Table.Cell>
                  <Table.Cell><RoleBadge account={account} /></Table.Cell>
                  <Table.Cell>{uidLabel(account)}</Table.Cell>
                  <Table.Cell>{formatDateTime(account.createdAt)}</Table.Cell>
                  <Table.Cell textAlign="end">{actions(account)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label="Users">
          {accounts.map((account) => (
            <ResourceCard key={account.id} title={account.username} actions={actions(account)}>
              <ResourceCardField label="Role"><RoleBadge account={account} /></ResourceCardField>
              <ResourceCardField label="UID">{uidLabel(account)}</ResourceCardField>
              <ResourceCardField label="Created">{formatDateTime(account.createdAt)}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
