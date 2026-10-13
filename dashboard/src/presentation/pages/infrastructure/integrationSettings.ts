/**
 * Returns `settings` with `key` set, or removed when `value` is empty, so a cleared field falls
 * back to the API's default instead of storing "" (contract sites-integrations.md). The input map
 * is never mutated, which keeps React state updates referentially honest.
 */
export function withIntegrationSetting(settings: Record<string, string>, key: string, value: string): Record<string, string> {
  const next = { ...settings }
  if (value) next[key] = value
  else delete next[key]
  return next
}

/**
 * Whether the Integration is a MAAS provisioner, the only kind whose settings page has the
 * provider-owned HTTP proxy and Subnet DNS tabs (decisions 056 and 058).
 */
export function isMaasProvisioner(integration: { kind: string; providerKind: string }): boolean {
  return integration.kind === 'provisioner' && integration.providerKind === 'maas'
}

/** Display names of the provider kinds swallow registers; the key is the domain value. */
const PROVIDER_LABELS: Record<string, string> = {
  maas: 'MAAS',
  prometheus: 'Prometheus',
  kubernetes: 'Kubernetes',
  slurm: 'Slurm',
}

/** The display name of a provider kind; an unknown kind (added to the API later) shows as-is. */
export function providerLabel(providerKind: string): string {
  return PROVIDER_LABELS[providerKind] ?? providerKind
}

/** Display names of the Integration roles; the key is the domain value (contract sites-integrations.md). */
const ROLE_LABELS: Record<string, string> = {
  provisioner: 'Provisioner',
  metrics: 'Metrics',
  platform: 'Platform',
}

/** The display name of an Integration role; an unknown role (added to the API later) shows as-is. */
export function roleLabel(kind: string): string {
  return ROLE_LABELS[kind] ?? kind
}
