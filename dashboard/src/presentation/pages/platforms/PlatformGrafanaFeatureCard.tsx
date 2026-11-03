import { Badge, Text } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import { FeatureToggleCard } from './FeatureToggleCard'

interface PlatformGrafanaFeatureCardProps {
  /** Where the Platform type runs the Grafana, completing "A read-only Grafana …", for example "on lab-ctl-1". */
  placement: string
  /** The dashboards it provisions, completing "… and includes …". */
  dashboards: string
  /** The deploy request's `monitoringDashboards`: on by default, as the API's omitted value is. */
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  /** Type-specific choices shown while the card is on, such as Slurm's install method. */
  children?: ReactNode
}

/**
 * The Platform Grafana card of a deploy wizard's Features step (platforms.md "Platform Monitoring
 * Dashboards"), shared by every Platform type so the same setting reads the same everywhere. Each
 * type says where its Grafana runs and which dashboards it gets; the access and lifecycle facts are
 * the setting's own: anonymous read-only access on the management network, and a switch the
 * backend carries out when turned on later but refuses to turn off after deployment.
 */
export function PlatformGrafanaFeatureCard({
  placement,
  dashboards,
  checked,
  onCheckedChange,
  children,
}: PlatformGrafanaFeatureCardProps) {
  return (
    <FeatureToggleCard
      title="Platform Grafana"
      description={`A read-only Grafana ${placement}. It shows only this Platform's Servers, read from the Site Prometheus, and includes ${dashboards}.`}
      checked={checked}
      onCheckedChange={onCheckedChange}
      status={<Badge variant="subtle" colorPalette={checked ? 'blue' : 'gray'}>{checked ? 'Included' : 'Not included'}</Badge>}
    >
      <div className="sw-platform-feature-stack">
        {children}
        <Text color="fg.muted" fontSize="sm">
          Anyone on the management network can view it without signing in. It can be turned on later, but not off after deployment.
        </Text>
      </div>
    </FeatureToggleCard>
  )
}
