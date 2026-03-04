import { Badge, type BadgeProps } from '@mantine/core'

type StatusType = 'success' | 'error' | 'warning' | 'info' | 'neutral' | 'running'

const STATUS_COLORS: Record<string, StatusType> = {
  // Mission statuses
  active: 'success',
  paused: 'warning',
  archived: 'neutral',
  draft: 'neutral',
  // Run statuses
  queued: 'info',
  running: 'running',
  succeeded: 'success',
  failed: 'error',
  canceled: 'neutral',
  // GPU health
  healthy: 'success',
  degraded: 'warning',
  critical: 'error',
  offline: 'neutral',
  // Alert severity/status
  warning: 'warning',
  acknowledged: 'info',
  resolved: 'neutral',
  // Asset
  unknown: 'neutral',
  // Plane
  connected: 'success',
  disconnected: 'error',
  // Job
  pending: 'info',
}

const TYPE_COLORS: Record<StatusType, string> = {
  success: 'green',
  error: 'red',
  warning: 'yellow',
  info: 'blue',
  neutral: 'gray',
  running: 'blue',
}

interface StatusBadgeProps extends Omit<BadgeProps, 'color'> {
  status: string
  label?: string
}

export function StatusBadge({ status, label, ...rest }: StatusBadgeProps) {
  const type = STATUS_COLORS[status] ?? 'neutral'
  const color = TYPE_COLORS[type]
  const variant = status === 'running' ? 'dot' : 'light'

  return (
    <Badge color={color} variant={variant} size="sm" {...rest}>
      {label ?? status}
    </Badge>
  )
}
