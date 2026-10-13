import { Badge, Button, Card, HStack, SimpleGrid, Stack, Text } from '@chakra-ui/react'
import { ExternalLink } from 'lucide-react'
import { METRIC_DESCRIPTORS } from '@/domain/monitoring/types'
import type { EffectiveExporterOwner } from '@/domain/monitoring/exporterOwner'
import type { ServerMonitoring } from '@/presentation/pages/servers/useServerMonitoring'
import { Alert } from '@/presentation/components/ui/alert'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'

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

/** One instant metric value; missing samples read as “No data” rather than zero. */
function MetricTile({ label, value }: { label: string; value: string | null }) {
  return (
    <Card.Root size="sm">
      <Card.Body gap="1">
        <Text fontSize="sm" color="fg.muted" fontWeight="medium">
          {label}
        </Text>
        <Text fontSize="2xl" fontWeight="bold" fontVariantNumeric="tabular-nums" lineHeight="1.2">
          {value ?? 'No data'}
        </Text>
      </Card.Body>
    </Card.Root>
  )
}

/**
 * Truthful current-value panel for one Server, shown above its usage charts on the Server
 * detail Monitoring tab.
 *
 * Loading keeps a stable skeleton; a backend failure remains a warning with explicit “No data”
 * tiles, except "no metrics backend registered", which the usage charts below explain once.
 * Exporter ownership stays visible when known. Grafana is an optional, secondary "Explore in
 * Grafana" link shown only when the metrics Integration configures it; swallow's own charts are
 * the primary history view.
 */
export function MonitoringCard({ monitoring }: { monitoring: ServerMonitoring }) {
  const { owner, gpu, metrics, grafana, metricsError, loading } = monitoring
  const descriptors = METRIC_DESCRIPTORS.filter((descriptor) => gpu || !descriptor.gpuOnly)
  return (
    <SectionSurface
      title="Current metrics"
      description="Latest samples for this Server."
      actions={!loading ? (
        <HStack gap="2" wrap="wrap">
          <Badge colorPalette={OWNER_COLOR[owner]} variant="subtle">
            {OWNER_LABEL[owner]}
          </Badge>
          {grafana && (
            <Button asChild variant="ghost" size="sm">
              <a href={grafana} target="_blank" rel="noreferrer">
                Explore in Grafana
                <ExternalLink size={16} />
              </a>
            </Button>
          )}
        </HStack>
      ) : undefined}
      className="sw-monitoring-panel"
    >
      {loading ? (
        <LoadingState rows={3} height={52} />
      ) : (
        <Stack gap="3">
          {owner === 'unmanaged' && (
            <Alert status="info" title="Exporters are unmanaged">
              Swallow will neither install nor remove exporters on this Server.
            </Alert>
          )}
          {metricsError && (
            <Alert status="warning" title="Metrics are unavailable">
              {metricsError}
            </Alert>
          )}
          <SimpleGrid minChildWidth="190px" gap="3">
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
          </SimpleGrid>
        </Stack>
      )}
    </SectionSurface>
  )
}
