import type { PlatformSpec } from './types'

/**
 * Whether a Platform's spec gives it its own Grafana (platforms.md "Platform Monitoring
 * Dashboards"): the `monitoringDashboards` setting is desired state that defaults to on, so an
 * omitted or `null` value is on and only an explicit `false` is off. A Platform without a spec yet
 * (not adopted) reads as on, the default its deployment had. The setting is the user's intent, not
 * proof that a Grafana runs: the `Platform Grafana` Monitoring Link appears once one is installed.
 */
export function wantsPlatformGrafana(spec: PlatformSpec | null): boolean {
  return spec?.settings.monitoringDashboards !== false
}
