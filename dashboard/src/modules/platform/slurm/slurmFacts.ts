import { wantsPlatformGrafana } from '@/domain/platform/monitoring'
import { platformCurrentSpec, settingObject, settingString } from '@/domain/platform/spec'
import type { ObservabilityInstallMethod, Platform } from '@/domain/platform/types'
import type { PlatformFact } from '@/modules/types'
import { platformGrafanaContainerHostFact, slurmContainerRuntimeLabel } from './slurmGrafanaCopy'

/**
 * How a Slurm Platform's own Grafana was installed on its primary controller, or null when the
 * spec gives it none (platforms.md "Platform Monitoring Dashboards"): the enabled monitoring
 * feature's `installMethod` (Omnistat or the Device Metrics Exporter, never both), and `native`
 * when neither is enabled.
 */
function platformGrafanaInstallMethod(platform: Platform): ObservabilityInstallMethod | null {
  if (!wantsPlatformGrafana(platformCurrentSpec(platform))) return null
  const feature = [platform.omnistat, platform.deviceMetricsExporter].find((candidate) => candidate?.enabled)
  return feature?.installMethod ?? 'native'
}

/**
 * Every container capability the deployment put on the cluster's hosts, in one fact as the deploy
 * wizard groups them: native OCI containers, the container engine on the compute and login nodes,
 * and the engine a container Platform Grafana put on the primary controller (otherwise a host
 * change the page would hide). Each Server's install state lives in its Software Assignment.
 */
function containerRuntimeFact(platform: Platform): string {
  const engine = platform.containerRuntime ?? null
  return [
    ...(platform.ociContainers ? ['Native OCI (srun --container)'] : []),
    ...(engine ? [`${slurmContainerRuntimeLabel(engine)} on compute and login nodes`] : []),
    ...(platformGrafanaInstallMethod(platform) === 'container' ? [platformGrafanaContainerHostFact(engine)] : []),
  ].join('; ') || 'None'
}

/**
 * The Slurm facts of the Platform details card: the cluster name and the pinned slurmrestd API
 * version from the spec's `slurm` settings (each states the API's default when the spec omits it),
 * then the containers and MPI the deployment set up, which the Platform record reports itself
 * (platforms.md "Container Runtime", "Native OCI Containers", "MPI (PMIx)"). A Platform not adopted
 * yet has no spec, so it shows only the recorded facts.
 */
export function slurmPlatformFacts(platform: Platform): PlatformFact[] {
  const spec = platformCurrentSpec(platform)
  const slurm = settingObject(spec?.settings, 'slurm')
  return [
    ...(spec
      ? [
          { label: 'Cluster name', value: settingString(slurm, 'clusterName') ?? 'Derived from the platform name' },
          { label: 'slurmrestd API version', value: settingString(slurm, 'apiVersion') ?? 'Auto-detected' },
        ]
      : []),
    { label: 'Container runtime', value: containerRuntimeFact(platform) },
    { label: 'MPI', value: platform.pmix ? 'OpenMPI with srun --mpi=pmix_v5' : 'Not set up' },
  ]
}
