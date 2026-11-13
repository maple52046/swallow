import { useState } from 'react'
import { Button, Field, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { PoolAccessPolicy, SudoSetting } from '@/domain/access/accounts'
import type { PoolRole } from '@/domain/user/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { POOL_ROLE_CHOICES, sudoSettingOptions, toPoolRole, toSudoSetting } from './poolAccessPresentation'

interface AddMemberDialogProps {
  poolId: string
  poolName: string
  /** The Pool's defaults, which the "Pool default" choices name. */
  policy: PoolAccessPolicy
  onClose: () => void
  /** Called after the member is added, with the toast title to show. */
  onAdded: (title: string) => void
}

/**
 * Adds an existing User to a Pool (pool-access.md `POST members`). Owners cannot list Users (that is
 * an admin read), so the username is typed; a name that is not a User comes back as `not_found` and
 * an existing member as `conflict`, both keeping the dialog open with the API's reason. The sudo
 * overrides start at the Pool default and are hidden for an owner, who always has sudo.
 */
export function AddMemberDialog({ poolId, poolName, policy, onClose, onAdded }: AddMemberDialogProps) {
  const { poolAccess } = useApp()
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<PoolRole>('member')
  const [sudo, setSudo] = useState<SudoSetting>('inherit')
  const [sudoPasswordless, setSudoPasswordless] = useState<SudoSetting>('inherit')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const name = username.trim()

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!name || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const member = await poolAccess.addMember(poolId, {
        username: name,
        role,
        sudoPasswordless,
        ...(role === 'member' ? { sudo } : {}),
      })
      onAdded(`${member.username} added to ${poolName}`)
    } catch (caught) {
      setError(failureOf(caught, 'The member could not be added.'))
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title={`Add a member to ${poolName}`}
      description="Add a person who already has a Swallow account. An admin creates accounts."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!name}>
            Add member
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Member not added">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="member-add-username">Username <Field.RequiredIndicator /></Field.Label>
          <Input
            id="member-add-username"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            maxLength={32}
            autoComplete="off"
            spellCheck={false}
            className="sw-mono"
            placeholder="e.g. alice"
            disabled={submitting}
            autoFocus
          />
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="member-add-role">Role</Field.Label>
          <Select
            id="member-add-role"
            aria-label="Role"
            value={role}
            onChange={(value) => setRole(toPoolRole(value))}
            options={POOL_ROLE_CHOICES}
            disabled={submitting}
          />
        </Field.Root>
        {role === 'member' && (
          <Field.Root>
            <Field.Label htmlFor="member-add-sudo">Sudo</Field.Label>
            <Select
              id="member-add-sudo"
              aria-label="Sudo"
              value={sudo}
              onChange={(value) => setSudo(toSudoSetting(value))}
              options={sudoSettingOptions(policy.memberSudo)}
              disabled={submitting}
            />
          </Field.Root>
        )}
        <Field.Root>
          <Field.Label htmlFor="member-add-passwordless">Sudo without password</Field.Label>
          <Select
            id="member-add-passwordless"
            aria-label="Sudo without password"
            value={sudoPasswordless}
            onChange={(value) => setSudoPasswordless(toSudoSetting(value))}
            options={sudoSettingOptions(policy.sudoPasswordless)}
            disabled={submitting}
          />
        </Field.Root>
        <Text color="fg.muted" fontSize="sm">
          The person can log in to this Pool&apos;s Servers with their SSH keys within a minute.
        </Text>
      </Stack>
    </Modal>
  )
}
