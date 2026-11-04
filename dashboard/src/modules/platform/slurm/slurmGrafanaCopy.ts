import type { SlurmContainerRuntime } from '@/domain/platform/types'

/** Display name of a Slurm container runtime choice; the domain value is what the request sends. */
export function slurmContainerRuntimeLabel(runtime: SlurmContainerRuntime): string {
  return runtime === 'podman' ? 'Podman' : 'Docker CE'
}

/**
 * Copy for the Platform Grafana's `container` install method, shared by the Slurm Features card and
 * the review card so both name the same host change.
 *
 * Per platforms.md ("Platform Monitoring Dashboards"), that method first puts a container runtime on
 * the primary controller: the container engine the deploy requests (`engine`, what the request's
 * `containerRuntime` sends), else a runtime the Server already has, else Docker CE. The wizard cannot
 * see the Server's existing Software Assignments, so without an engine it names Docker CE and says an
 * existing engine is reused instead.
 */
export function platformGrafanaContainerCopy(
  engine: SlurmContainerRuntime | '',
  primaryControllerName: string,
): { option: string; review: string } {
  if (engine) {
    const label = slurmContainerRuntimeLabel(engine)
    return {
      option: `Run Grafana in a container on ${primaryControllerName}, installing ${label} there, the container engine chosen above.`,
      review: `In a container on ${primaryControllerName}, which gets ${label} to run it`,
    }
  }
  return {
    option: `Run Grafana in a container on ${primaryControllerName}, installing Docker CE there unless it already has a container engine.`,
    review: `In a container on ${primaryControllerName}, which gets Docker CE to run it unless it already has a container engine`,
  }
}
