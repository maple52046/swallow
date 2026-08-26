import { Badge } from '@radix-ui/themes'

/** The Radix accent colours this badge maps onto. */
type RadixColor = 'green' | 'red' | 'yellow' | 'blue' | 'gray'

type StatusType = 'success' | 'error' | 'warning' | 'info' | 'neutral' | 'running'

/**
 * Maps a domain status string to a semantic type. Kept broad so one badge serves every
 * status value set in the platform (mission, run, GPU health, alert, plane, job).
 */
const STATUS_TYPES: Record<string, StatusType> = {
  active: 'success',
  paused: 'warning',
  archived: 'neutral',
  draft: 'neutral',
  queued: 'info',
  running: 'running',
  succeeded: 'success',
  failed: 'error',
  canceled: 'neutral',
  healthy: 'success',
  degraded: 'warning',
  critical: 'error',
  offline: 'neutral',
  warning: 'warning',
  acknowledged: 'info',
  resolved: 'neutral',
  unknown: 'neutral',
  connected: 'success',
  disconnected: 'error',
  pending: 'info',
  // Operation execution states and per-task event results share this badge; absence of
  // evidence (indeterminate) is a warning, never an error.
  indeterminate: 'warning',
  ok: 'success',
  changed: 'info',
  skipped: 'neutral',
  unreachable: 'error',
}

const TYPE_COLORS: Record<StatusType, RadixColor> = {
  success: 'green',
  error: 'red',
  warning: 'yellow',
  info: 'blue',
  neutral: 'gray',
  running: 'blue',
}

interface StatusBadgeProps {
  /** Domain status value (not a display label); the mapping owns colour choice. */
  status: string
  /** Optional display override; defaults to the status value itself. */
  label?: string
}

/**
 * The shared status badge for every status value set in the dashboard.
 *
 * `status` is the domain value from the platform glossary, not a display label; the badge
 * always carries text, so status is never conveyed by colour alone. Reuse this rather
 * than colouring badges per page (coding-style DRY gate).
 */
export function StatusBadge({ status, label }: StatusBadgeProps) {
  const type = STATUS_TYPES[status] ?? 'neutral'
  return (
    <Badge color={TYPE_COLORS[type]} variant="soft">
      {label ?? status}
    </Badge>
  )
}
