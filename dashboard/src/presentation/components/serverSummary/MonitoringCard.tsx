import { Badge, Button, Card, Flex, Heading, HStack, SimpleGrid, Stack, Text } from '@chakra-ui/react'
import { ExternalLink } from 'lucide-react'
import { METRIC_DESCRIPTORS } from '@/domain/monitoring/types'
import type { EffectiveExporterOwner } from '@/domain/monitoring/exporterOwner'
import type { ServerMonitoring } from '@/presentation/pages/servers/useServerMonitoring'
import { Alert } from '@/presentation/components/ui/alert'

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

/** One instant metric value; missing samples read as "No data" rather than zero. */
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
 * Grafana-inspired instant metric panel.
 *
 * Missing samples stay "No data", exporter ownership remains visible as a badge,
 * and the backend-provided Grafana URL is opened as-is (no invented UIDs). GPU-only
 * metrics are shown only when the Server reports a GPU.
 */
export function MonitoringCard({ monitoring }: { monitoring: ServerMonitoring }) {
  const { owner, gpu, metrics, grafana, metricsError } = monitoring
  const descriptors = METRIC_DESCRIPTORS.filter((descriptor) => gpu || !descriptor.gpuOnly)
  return (
    <Card.Root>
      <Card.Body gap="4">
        <Flex justify="space-between" align="center" gap="3" wrap="wrap">
          <HStack gap="3">
            <Heading size="sm">Current metrics</Heading>
            <Badge colorPalette={OWNER_COLOR[owner]} variant="subtle">
              {OWNER_LABEL[owner]}
            </Badge>
          </HStack>
          {grafana && (
            <Button asChild variant="outline" size="sm">
              <a href={grafana} target="_blank" rel="noreferrer">
                Open Grafana
                <ExternalLink size={16} />
              </a>
            </Button>
          )}
        </Flex>
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
      </Card.Body>
    </Card.Root>
  )
}
