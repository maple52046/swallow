import { useState } from 'react'
import { Button, HStack, Stack, Switch, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { PoolAccessPolicy } from '@/domain/access/accounts'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'

interface PoolSudoDefaultsSectionProps {
  poolId: string
  /** The Pool's current defaults as the API returned them; the draft starts from them. */
  policy: PoolAccessPolicy
  /** Whether the caller is an admin or an owner of the Pool; members see the switches read-only. */
  canManage: boolean
  /** Called after a save, so the page re-reads the policy and the members' effective values. */
  onSaved: () => void
}

/**
 * A Pool's Sudo Policy defaults on its access page (pool-access.md `access-policy`): whether members
 * have sudo, and whether sudo asks for a password, unless a membership overrides them. Owners always
 * have sudo; only the password default applies to them.
 *
 * The switches edit a local draft and "Save" writes both values together, because together they
 * decide who can become root on the Pool's Servers; a stray click must not change that on its own.
 * The draft resets whenever the page re-reads the policy (the section is keyed by it).
 */
export function PoolSudoDefaultsSection({ poolId, policy, canManage, onSaved }: PoolSudoDefaultsSectionProps) {
  const { poolAccess } = useApp()
  const { showToast } = useToast()
  const [memberSudo, setMemberSudo] = useState(policy.memberSudo)
  const [sudoPasswordless, setSudoPasswordless] = useState(policy.sudoPasswordless)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const changed = memberSudo !== policy.memberSudo || sudoPasswordless !== policy.sudoPasswordless

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      await poolAccess.setPolicy(poolId, { memberSudo, sudoPasswordless })
      showToast({ tone: 'success', title: 'Sudo defaults saved' })
      onSaved()
    } catch (caught) {
      setError(failureOf(caught, 'The sudo defaults could not be saved.'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionSurface
      title="Sudo defaults"
      description="What every member gets unless their membership overrides it. Owners always have sudo."
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Sudo defaults not saved">
            <FailureMessage failure={error} />
          </Alert>
        )}
        <Switch.Root checked={memberSudo} onCheckedChange={(details) => setMemberSudo(details.checked === true)} disabled={!canManage || saving}>
          <Switch.HiddenInput />
          <Switch.Control><Switch.Thumb /></Switch.Control>
          <Switch.Label>Members have sudo: {memberSudo ? 'yes' : 'no'}</Switch.Label>
        </Switch.Root>
        <Switch.Root checked={sudoPasswordless} onCheckedChange={(details) => setSudoPasswordless(details.checked === true)} disabled={!canManage || saving}>
          <Switch.HiddenInput />
          <Switch.Control><Switch.Thumb /></Switch.Control>
          <Switch.Label>Sudo without a password: {sudoPasswordless ? 'yes' : 'no'}</Switch.Label>
        </Switch.Root>
        {canManage ? (
          <HStack>
            <Button colorPalette="brand" size="sm" onClick={() => void save()} loading={saving} disabled={!changed}>
              Save defaults
            </Button>
            {changed && (
              <Button variant="ghost" size="sm" disabled={saving} onClick={() => { setMemberSudo(policy.memberSudo); setSudoPasswordless(policy.sudoPasswordless) }}>
                Discard
              </Button>
            )}
          </HStack>
        ) : (
          <Text color="fg.muted" fontSize="sm">Only the Pool&apos;s owners and admins can change these.</Text>
        )}
      </Stack>
    </SectionSurface>
  )
}
