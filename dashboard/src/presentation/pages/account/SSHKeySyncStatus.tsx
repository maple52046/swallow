import { Badge, HStack, Spinner, Stack, Text } from '@chakra-ui/react'
import type { SSHKey } from '@/domain/access/types'
import { formatDateTime } from '@/shared/utils/time'
import { syncStateLabel, syncStatePalette, worstSyncState } from './sshKeyPresentation'

interface SSHKeySyncStatusProps {
  sshKey: SSHKey
  /** Resolves an Integration id to its operator-facing name; falls back to the id. */
  integrationName: (integrationId: string) => string
  /**
   * `list` shows one row per provisioner with its reason (the Deployment Key card); `summary`
   * shows the single state that most needs attention (Access Key table cells).
   */
  variant: 'list' | 'summary'
  /**
   * The key is being re-read because it has a pending entry (usePendingSyncRefresh). Pending
   * entries then show a decorative spinner; the "Pending" text remains the status of record.
   */
  following?: boolean
}

/**
 * Shows whether an SSH Key has been realized in each provisioner, so an operator knows whether
 * Servers deployed next will authorize it. It is the one presentation of sync status on the SSH
 * Keys page, used for both the Deployment Key and every Access Key. States are always spelled out
 * in text (badges carry a label, not only a colour), and a failure's reason is always shown as
 * text so it is reachable without hover. Both variants are polite live regions, so a pending entry
 * that settles while followed is announced without moving focus.
 */
export function SSHKeySyncStatus({ sshKey, integrationName, variant, following = false }: SSHKeySyncStatusProps) {
  if (sshKey.providerSync.length === 0) {
    return <Text color="fg.muted" fontSize="sm">No provisioner registered</Text>
  }

  if (variant === 'summary') {
    const state = worstSyncState(sshKey) ?? 'pending'
    const failures = sshKey.providerSync.filter((entry) => entry.state === 'failed')
    const badge = (
      <HStack gap="1.5" aria-live="polite">
        <Badge colorPalette={syncStatePalette(state)} variant="subtle">
          {syncStateLabel(state)}
          {sshKey.providerSync.length > 1 ? ` · ${sshKey.providerSync.filter((entry) => entry.state === 'synced').length}/${sshKey.providerSync.length}` : ''}
        </Badge>
        {following && state === 'pending' && <Spinner size="xs" color="fg.muted" aria-hidden />}
      </HStack>
    )
    if (failures.length === 0) return badge
    // The first failure's reason is shown as text rather than in a tooltip: a badge is not
    // focusable, so a hover-only reason would be unreachable from the keyboard.
    const first = failures[0]
    return (
      <Stack gap="0.5" align="flex-start">
        {badge}
        <Text color="fg.error" fontSize="xs" lineClamp={2}>
          {integrationName(first.integrationId)}: {first.error ?? 'failed'}
        </Text>
      </Stack>
    )
  }

  return (
    <Stack as="ul" gap="2" listStyleType="none" m="0" p="0" aria-label="Provisioner sync status" aria-live="polite">
      {sshKey.providerSync.map((entry) => (
        <Stack as="li" key={entry.integrationId} gap="0.5">
          <HStack gap="2" wrap="wrap">
            <Text fontWeight="medium">{integrationName(entry.integrationId)}</Text>
            <Badge colorPalette={syncStatePalette(entry.state)} variant="subtle">
              {syncStateLabel(entry.state)}
            </Badge>
            {following && entry.state === 'pending' && (
              <HStack gap="1" color="fg.muted" fontSize="xs">
                <Spinner size="xs" aria-hidden />
                <Text as="span">Checking…</Text>
              </HStack>
            )}
            {entry.syncedAt && (
              <Text color="fg.muted" fontSize="xs">
                Last synced {formatDateTime(entry.syncedAt)}
              </Text>
            )}
          </HStack>
          {entry.state === 'failed' && entry.error && (
            <Text color="fg.error" fontSize="sm">{entry.error}</Text>
          )}
          {entry.state === 'unsupported' && (
            <Text color="fg.muted" fontSize="sm">
              This provisioner cannot hold SSH keys; authorize the key on its Servers another way.
            </Text>
          )}
        </Stack>
      ))}
    </Stack>
  )
}
