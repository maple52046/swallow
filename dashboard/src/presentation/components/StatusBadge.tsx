import { Badge } from '@chakra-ui/react'

/** Chakra colour palette applied to a status family. Colour is a supplementary cue only. */
type StatusPalette = 'green' | 'red' | 'orange' | 'blue' | 'gray' | 'purple' | 'teal' | 'yellow'

/**
 * Maps a lowercased domain status to a semantic colour family.
 *
 * The value drives colour, but the badge always renders the status text, so
 * `unknown`, `warning`, and failure stay distinguishable without relying on
 * colour. Durable Operation (schema v3) statuses read as attention/partial rather
 * than a clean success or hard failure.
 */
const STATUS_COLORS: Record<string, StatusPalette> = {
  active: 'green', healthy: 'green', succeeded: 'green', ok: 'green', connected: 'green', up: 'green', ready: 'green',
  failed: 'red', critical: 'red', disconnected: 'red', unreachable: 'red', down: 'red', broken: 'red',
  warning: 'orange', degraded: 'orange', indeterminate: 'orange',
  requires_attention: 'orange', partially_succeeded: 'orange', canceling: 'orange',
  waiting_external: 'blue', waiting_dependency: 'blue',
  pending: 'blue', queued: 'blue', running: 'blue', changed: 'blue', info: 'blue', firing: 'red',
  acknowledged: 'purple', suppressed: 'purple',
  canceled: 'gray', resolved: 'gray', unknown: 'gray', offline: 'gray', archived: 'gray', draft: 'gray', skipped: 'gray',
}

interface StatusBadgeProps {
  status: string
  label?: string
}

/**
 * Shared status label for all bounded contexts.
 *
 * The domain `status` value selects a semantic colour family while the visible
 * text always carries the status meaning, so status is never conveyed by colour
 * alone. When `label` is omitted the raw status is humanised (underscores to
 * spaces).
 */
export function StatusBadge({ status, label }: StatusBadgeProps) {
  const palette = STATUS_COLORS[status.toLocaleLowerCase()] ?? 'gray'
  return (
    <Badge colorPalette={palette} variant="subtle" textTransform="capitalize">
      {label ?? status.replaceAll('_', ' ')}
    </Badge>
  )
}
