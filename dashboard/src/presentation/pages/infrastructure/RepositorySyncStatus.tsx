import { HStack, Stack, Text } from '@chakra-ui/react'
import type { LocalRepositorySyncState } from '@/domain/localRepository/types'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { syncStatePresentation } from './localRepositoryPresentation'

interface RepositorySyncStatusProps {
  state: LocalRepositorySyncState
  /** The provider's reason, shown as visible text under the badge; `null` when it gave none. */
  message: string | null
  /**
   * Whether the worker converges the repository (its spec serves deployments). When it does not,
   * the state is the last observation and reads without a spinner, a failure's colour, or the
   * progress message of a fetch that no longer runs.
   */
  synchronizing: boolean
}

/**
 * The observed state of one Repository Source, declared file, or declared image on the Local
 * repository page: a labelled badge, a spinner while a fetch runs or waits, and the provider's
 * message as text, so a failure's reason is readable without hovering and the state never rests on
 * colour. Used by the sources, files, and images tables so they read the same.
 */
export function RepositorySyncStatus({ state, message, synchronizing }: RepositorySyncStatusProps) {
  const presentation = syncStatePresentation(state, synchronizing)
  return (
    // A mobile card's status slot is a flex item beside the title; shrinking it would push the
    // spinner past the card's edge.
    <Stack gap="1" align="flex-start" minW="0" flexShrink={0}>
      <HStack gap="1.5">
        <StatusBadge status={presentation.badgeStatus} label={presentation.label} />
        {presentation.inProgress && <InProgressSpinner />}
      </HStack>
      {message && presentation.messageTone && (
        <Text fontSize="xs" color={presentation.messageTone === 'error' ? 'fg.error' : 'fg.muted'} overflowWrap="anywhere">
          {message}
        </Text>
      )}
    </Stack>
  )
}

/**
 * The provider's message as a field of a mobile card, where the status slot beside the title has no
 * room for it: labelled "Failure" in error text when it is a failure's reason the worker retries,
 * "Message" in muted text otherwise (including a failure recorded before the repository stopped
 * synchronizing). Renders nothing without a message, or when it described a fetch that no longer
 * runs (see `syncStatePresentation`).
 */
export function RepositorySyncMessageField({ state, message, synchronizing }: RepositorySyncStatusProps) {
  const { messageTone } = syncStatePresentation(state, synchronizing)
  if (!message || !messageTone) return null
  return (
    <ResourceCardField label={messageTone === 'error' ? 'Failure' : 'Message'}>
      {/* A provider's reason often holds a long URL; breaking it anywhere keeps the card's column narrow. */}
      <Text color={messageTone === 'error' ? 'fg.error' : 'fg.muted'} overflowWrap="anywhere">{message}</Text>
    </ResourceCardField>
  )
}
