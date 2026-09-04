import { Label } from '@patternfly/react-core'

/** PatternFly semantic label color for platform status families. */
type StatusColor = 'green' | 'red' | 'orange' | 'blue' | 'grey' | 'purple' | 'teal' | 'yellow'

const STATUS_COLORS: Record<string, StatusColor> = {
  active: 'green', healthy: 'green', succeeded: 'green', ok: 'green', connected: 'green', up: 'green', ready: 'green',
  failed: 'red', critical: 'red', disconnected: 'red', unreachable: 'red', down: 'red', broken: 'red',
  warning: 'orange', degraded: 'orange', indeterminate: 'orange',
  // Durable Operation (schema v3) statuses. Text always carries the meaning; colour is a
  // supplementary cue. requires_attention and partially_succeeded read as attention/partial
  // rather than a clean success or a hard failure.
  requires_attention: 'orange', partially_succeeded: 'orange', canceling: 'orange',
  waiting_external: 'blue', waiting_dependency: 'blue',
  pending: 'blue', queued: 'blue', running: 'blue', changed: 'blue', info: 'blue', firing: 'red',
  acknowledged: 'purple', suppressed: 'purple',
  canceled: 'grey', resolved: 'grey', unknown: 'grey', offline: 'grey', archived: 'grey', draft: 'grey', skipped: 'grey',
}

interface StatusBadgeProps {
  status: string
  label?: string
}

/**
 * Shared status label for all bounded contexts.
 * The domain value selects a semantic PatternFly color, while visible text always carries
 * the status meaning so unknown, warning, and failure are distinguishable without color.
 */
export function StatusBadge({ status, label }: StatusBadgeProps) {
  return <Label color={STATUS_COLORS[status.toLocaleLowerCase()] ?? 'grey'}>{label ?? status.replaceAll('_', ' ')}</Label>
}
