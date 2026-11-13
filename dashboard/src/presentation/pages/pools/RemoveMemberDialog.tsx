import { useState } from 'react'
import { Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { PoolMember } from '@/domain/access/accounts'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'

interface RemoveMemberDialogProps {
  poolId: string
  poolName: string
  member: PoolMember
  /** Whether the caller removes itself, which ends its own access to the Pool. */
  isSelf: boolean
  onClose: () => void
  /** Called after the membership is removed, with the toast title to show. */
  onRemoved: (title: string) => void
}

/**
 * Confirms removing a membership (pool-access.md `DELETE`). The consequence is stated first: within
 * a minute the person can no longer log in to the Pool's Servers or use sudo there; SSH sessions
 * already open are not ended, and their files stay. Removing oneself is allowed (an owner may leave)
 * and the dialog says that the caller loses the Pool with it.
 */
export function RemoveMemberDialog({ poolId, poolName, member, isSelf, onClose, onRemoved }: RemoveMemberDialogProps) {
  const { poolAccess } = useApp()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const confirm = async () => {
    if (submitting) return
    setSubmitting(true)
    setError(null)
    try {
      await poolAccess.removeMember(poolId, member.userId)
      onRemoved(`${member.username} removed from ${poolName}`)
    } catch (caught) {
      setError(failureOf(caught, 'The member could not be removed.'))
      setSubmitting(false)
    }
  }

  return (
    <ConfirmDialog open title={`Remove ${member.username} from ${poolName}`} confirmLabel="Remove member" onConfirm={() => void confirm()} onCancel={onClose} busy={submitting}>
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Member not removed">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Text>
          Within a minute <strong>{member.username}</strong> can no longer log in to {poolName}&apos;s Servers or use sudo there. SSH sessions already open are not ended, and their files stay.
        </Text>
        {isSelf && (
          <Alert status="warning" title="You are removing yourself">
            You lose access to this Pool as soon as you confirm. Only another owner or an admin can add you back.
          </Alert>
        )}
      </Stack>
    </ConfirmDialog>
  )
}
