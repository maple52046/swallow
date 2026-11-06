import { HStack, Stack, Text } from '@chakra-ui/react'
import { isSyncInProgress, type LocalRepositorySyncState } from '@/domain/localRepository/types'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { syncStatePresentation } from './localRepositoryPresentation'

interface RepositorySyncStatusProps {
  state: LocalRepositorySyncState
  /** The provider's reason, shown as visible text under the badge; `null` when it gave none. */
  message: string | null
}

/**
 * The observed state of one Repository Source, declared file, or declared image on the Local
 * repository page: a labelled badge, a spinner while a fetch runs or waits, and the provider's
 * message as text, so a failure's reason is readable without hovering and the state never rests on
 * colour. Used by the sources, files, and images tables so they read the same.
 */
export function RepositorySyncStatus({ state, message }: RepositorySyncStatusProps) {
  const presentation = syncStatePresentation(state)
  return (
    // A mobile card's status slot is a flex item beside the title; shrinking it would push the
    // spinner past the card's edge.
    <Stack gap="1" align="flex-start" minW="0" flexShrink={0}>
      <HStack gap="1.5">
        <StatusBadge status={presentation.badgeStatus} label={presentation.label} />
        {isSyncInProgress(state) && <InProgressSpinner />}
      </HStack>
      {message && (
        <Text fontSize="xs" color={state === 'failed' ? 'fg.error' : 'fg.muted'} overflowWrap="anywhere">
          {message}
        </Text>
      )}
    </Stack>
  )
}
