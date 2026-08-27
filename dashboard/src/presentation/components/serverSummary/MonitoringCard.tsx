import { Alert, AlertVariant, Button, Card, CardBody, CardTitle, Gallery, Label } from '@patternfly/react-core'
import { ExternalLinkAltIcon } from '@patternfly/react-icons'
import { METRIC_DESCRIPTORS } from '@/domain/monitoring/types'
import type { EffectiveExporterOwner } from '@/domain/monitoring/exporterOwner'
import type { ServerMonitoring } from '@/presentation/pages/servers/useServerMonitoring'

const OWNER_LABEL: Record<EffectiveExporterOwner, string> = { ansible: 'Managed by Ansible', k8s: 'Managed by Kubernetes', unmanaged: 'Unmanaged' }
const OWNER_COLOR: Record<EffectiveExporterOwner, 'green' | 'blue' | 'grey'> = { ansible: 'green', k8s: 'blue', unmanaged: 'grey' }

function MetricTile({ label, value }: { label: string; value: string | null }) {
  return <Card isCompact><CardTitle>{label}</CardTitle><CardBody><strong className="sw-resource-value">{value ?? 'No data'}</strong></CardBody></Card>
}

/**
 * Grafana-inspired instant metric panel. Missing samples stay No data, exporter ownership
 * remains visible, and the backend-provided Grafana URL is opened without inventing UIDs.
 */
export function MonitoringCard({ monitoring }: { monitoring: ServerMonitoring }) {
  const { owner, gpu, metrics, grafana, metricsError } = monitoring
  const descriptors = METRIC_DESCRIPTORS.filter((descriptor) => gpu || !descriptor.gpuOnly)
  return <section className="sw-section"><div className="sw-section-header"><div><CardTitle>Current metrics</CardTitle><Label color={OWNER_COLOR[owner]}>{OWNER_LABEL[owner]}</Label></div>{grafana && <Button component="a" href={grafana} target="_blank" rel="noreferrer" variant="secondary" icon={<ExternalLinkAltIcon />} iconPosition="end">Open Grafana</Button>}</div><div className="sw-monitoring-body">{owner === 'unmanaged' && <Alert variant={AlertVariant.info} title="Exporters are unmanaged" isInline>Swallow will neither install nor remove exporters on this Server.</Alert>}{metricsError && <Alert variant={AlertVariant.warning} title="Metrics are unavailable" isInline>{metricsError}</Alert>}<Gallery hasGutter minWidths={{ default: '190px' }}>{descriptors.map((descriptor) => { const value = metrics[descriptor.name]; return <MetricTile key={descriptor.name} label={descriptor.label} value={value === undefined ? null : descriptor.format(value)} /> })}</Gallery></div></section>
}
