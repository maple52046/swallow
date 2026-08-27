import { Badge, Callout, Card, Flex, Grid, Heading, Link, Text } from '@radix-ui/themes'
import { InfoCircledIcon } from '@radix-ui/react-icons'
import { METRIC_DESCRIPTORS } from '@/domain/monitoring/types'
import type { EffectiveExporterOwner } from '@/domain/monitoring/exporterOwner'
import type { ServerMonitoring } from '@/presentation/pages/servers/useServerMonitoring'

/** Human label per effective owner; text carries the meaning, never colour alone. */
const OWNER_LABEL: Record<EffectiveExporterOwner, string> = {
  ansible: 'Managed by Ansible',
  k8s: 'Managed by Kubernetes',
  unmanaged: 'Unmanaged',
}

const OWNER_COLOR: Record<EffectiveExporterOwner, 'green' | 'blue' | 'gray'> = {
  ansible: 'green',
  k8s: 'blue',
  unmanaged: 'gray',
}

/**
 * One metric tile: a big value with its label, or an explicit "no data" when the metrics
 * store had no answer. "no data" is never rendered as 0, because an unscraped server and a
 * genuinely idle one are different facts.
 */
function MetricTile({ label, value }: { label: string; value: string | null }) {
  return (
    <Card>
      <Text size="1" color="gray">
        {label}
      </Text>
      <Text as="div" size="6" weight="bold" color={value ? undefined : 'gray'}>
        {value ?? 'no data'}
      </Text>
    </Card>
  )
}

/**
 * The server's monitoring panel: the effective exporter owner, the current metric values,
 * and a Grafana deep link. Cards render even with no data — an empty panel is the correct,
 * explicit state for a server with no exporter (for example a locked, unmanaged machine),
 * rather than hiding monitoring entirely.
 *
 * Host metrics (CPU, memory) show for every server; GPU metrics show only for GPU servers.
 */
export function MonitoringCard({ monitoring }: { monitoring: ServerMonitoring }) {
  const { owner, gpu, metrics, grafana, metricsError } = monitoring
  const descriptors = METRIC_DESCRIPTORS.filter((descriptor) => gpu || !descriptor.gpuOnly)

  return (
    <Card>
      <Flex justify="between" align="center" mb="2" gap="3" wrap="wrap">
        <Heading as="h2" size="3">
          Monitoring
        </Heading>
        <Flex align="center" gap="2" wrap="wrap">
          <Badge color={OWNER_COLOR[owner]} variant="soft">
            {OWNER_LABEL[owner]}
          </Badge>
          {grafana && (
            <Link href={grafana} target="_blank" rel="noreferrer" size="2">
              Open in Grafana
            </Link>
          )}
        </Flex>
      </Flex>

      {owner === 'unmanaged' && (
        <Callout.Root color="gray" mb="3" size="1">
          <Callout.Icon>
            <InfoCircledIcon />
          </Callout.Icon>
          <Callout.Text>
            Exporters here are not managed by swallow — the machine is locked or an operator
            installed them by hand. swallow will neither install nor remove them.
          </Callout.Text>
        </Callout.Root>
      )}

      {metricsError && (
        <Callout.Root color="gray" mb="3" size="1">
          <Callout.Icon>
            <InfoCircledIcon />
          </Callout.Icon>
          <Callout.Text>No metrics data yet: {metricsError}</Callout.Text>
        </Callout.Root>
      )}

      <Grid columns={{ initial: '2', sm: '3' }} gap="3">
        {descriptors.map((descriptor) => {
          const value = metrics[descriptor.name]
          return (
            <MetricTile
              key={descriptor.name}
              label={descriptor.label}
              value={value === undefined ? null : descriptor.format(value)}
            />
          )
        })}
      </Grid>
    </Card>
  )
}
