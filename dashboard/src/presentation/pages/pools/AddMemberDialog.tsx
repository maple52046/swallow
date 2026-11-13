import { useEffect, useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { PoolAccessPolicy, PoolMemberCandidate, SudoSetting } from '@/domain/access/accounts'
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
  /**
   * The Pool's defaults, which the "Pool default" choices name. A caller that has not read them
   * (the Pools list) omits them, and the dialog reads them itself.
   */
  policy?: PoolAccessPolicy
  onClose: () => void
  /** Called after the member is added, with the toast title to show. */
  onAdded: (title: string) => void
}

/** How long typing pauses before the matching accounts are read again. */
const CANDIDATE_DEBOUNCE_MS = 250

/** The matching accounts as the dialog last read them; `null` before the first answer. */
type CandidateState = { query: string; items: readonly PoolMemberCandidate[] } | null

/**
 * Adds an existing User to a Pool (pool-access.md `POST members`), from the Pool's members page or
 * from its row on the Pools page. Owners cannot list Users (that is an admin read), so the dialog
 * offers the accounts that are not members yet from `member-candidates` (at most 20, filtered by
 * what is typed, re-read after a short pause); picking one fills the username, which can also be
 * typed. A name that is not a User comes back as `not_found` and an existing member as `conflict`,
 * both keeping the dialog open with the API's reason. A failed candidate read only hides the
 * suggestions. The sudo overrides start at the Pool default and are hidden for an owner, who always
 * has sudo.
 */
export function AddMemberDialog({ poolId, poolName, policy, onClose, onAdded }: AddMemberDialogProps) {
  const { poolAccess } = useApp()
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<PoolRole>('member')
  const [sudo, setSudo] = useState<SudoSetting>('inherit')
  const [sudoPasswordless, setSudoPasswordless] = useState<SudoSetting>('inherit')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [loadedPolicy, setLoadedPolicy] = useState<PoolAccessPolicy | null>(null)
  const [candidates, setCandidates] = useState<CandidateState>(null)
  const name = username.trim()
  const defaults = policy ?? loadedPolicy

  // Reads the Pool's defaults once when the caller did not pass them; until then the "Pool default"
  // choices read "no", the zero value of a Pool without stored defaults.
  useEffect(() => {
    if (policy) return
    let cancelled = false
    poolAccess.getPolicy(poolId)
      .then((read) => {
        if (!cancelled) setLoadedPolicy(read)
      })
      .catch(() => {
        // The defaults only label the "Pool default" choices; the add itself does not need them.
      })
    return () => {
      cancelled = true
    }
  }, [policy, poolAccess, poolId])

  // Re-reads the matching accounts after typing pauses; the cleanup cancels a pending read so a slow
  // answer for an earlier query never replaces a newer one.
  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(() => {
      poolAccess.listCandidates(poolId, name)
        .then((items) => {
          if (!cancelled) setCandidates({ query: name, items })
        })
        .catch(() => {
          if (!cancelled) setCandidates({ query: name, items: [] })
        })
    }, CANDIDATE_DEBOUNCE_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [poolAccess, poolId, name])

  // An exact match is already chosen, so it is not offered again.
  const suggestions = (candidates?.items ?? []).filter((candidate) => candidate.username !== name)

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
            placeholder="Type to find an account"
            disabled={submitting}
            autoFocus
          />
          {candidates && candidates.items.length === 0 && (
            <Field.HelperText>
              {candidates.query ? `No account that is not already a member matches “${candidates.query}”.` : 'Every account is already a member of this Pool.'}
            </Field.HelperText>
          )}
        </Field.Root>
        {suggestions.length > 0 && (
          <Stack gap="1.5" role="group" aria-label="Matching accounts">
            <Text color="fg.muted" fontSize="sm">Matching accounts</Text>
            <HStack gap="1.5" wrap="wrap">
              {suggestions.map((candidate) => (
                <Button
                  key={candidate.userId}
                  size="xs"
                  variant="outline"
                  className="sw-mono"
                  aria-label={`Choose ${candidate.username}`}
                  disabled={submitting}
                  onClick={() => setUsername(candidate.username)}
                >
                  {candidate.username}
                </Button>
              ))}
            </HStack>
          </Stack>
        )}
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
              options={sudoSettingOptions(defaults?.memberSudo ?? false)}
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
            options={sudoSettingOptions(defaults?.sudoPasswordless ?? false)}
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
