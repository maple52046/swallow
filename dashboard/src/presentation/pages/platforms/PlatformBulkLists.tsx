import { List, Stack, Text } from '@chakra-ui/react'
import type { Platform } from '@/domain/platform/types'
import { Alert } from '@/presentation/components/ui/alert'
import type { PlatformBulkOutcome } from './usePlatformBulkActions'

/**
 * The Platforms a bulk removal dialog will act on, under a visible label that doubles as the list's
 * accessible name. Every name is listed in a bounded, scrollable area (`sw-dialog-name-list`) rather
 * than collapsed into a count, because the operator is confirming exactly these records.
 */
export function PlatformNameList({ label, platforms }: { label: string; platforms: readonly Platform[] }) {
  return (
    <Stack gap="1">
      <Text fontWeight="medium">{`${label} (${platforms.length})`}</Text>
      <List.Root aria-label={label} className="sw-dialog-name-list" listStyle="none" ps="0">
        {platforms.map((platform) => (
          <List.Item key={platform.id}>{platform.name}</List.Item>
        ))}
      </List.Root>
    </Stack>
  )
}

/**
 * Shown when every request of a bulk removal failed, so the dialog stays open with each Platform's
 * error instead of a single generic message; nothing renders while `failures` is empty.
 */
export function PlatformBulkFailures({ failures }: { failures: readonly PlatformBulkOutcome[] }) {
  if (failures.length === 0) return null
  return (
    <Alert status="error" title="Every selected request failed">
      <List.Root aria-label="Failed platform requests" ps="4">
        {failures.map((failure) => (
          <List.Item key={failure.platformId}>{failure.platformName}: {failure.message}</List.Item>
        ))}
      </List.Root>
    </Alert>
  )
}
