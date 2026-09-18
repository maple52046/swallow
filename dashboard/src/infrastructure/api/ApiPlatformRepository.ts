import type { KubernetesExplorer, PlatformRepository } from '@/application/ports/PlatformRepository'
import type {
  Platform,
  DeployPlatformInput,
  DeployPlatformResult,
  MembershipReport,
  SlurmCluster,
  UninstallPlatformOptions,
  MinimumResources,
  SlurmDeploymentRequirement,
} from '@/domain/platform/types'
import type {
  KubernetesApplication,
  KubernetesApplicationKind,
  KubernetesApplyResult,
  KubernetesClusterSummary,
  KubernetesConfigResource,
  KubernetesIngress,
  KubernetesNamespace,
  KubernetesNode,
  KubernetesPersistentVolumeClaim,
  KubernetesPod,
  KubernetesPodLogs,
  KubernetesService,
} from '@/domain/platform/kubernetes'
import { ApiRequestError, apiRequest } from './client'

/** Path prefix for a platform's Kubernetes explorer, with the id URL-encoded once. */
function kubernetesBase(platformId: string): string {
  return `/api/v1/platforms/${encodeURIComponent(platformId)}/kubernetes`
}

/** Appends an optional `namespace` query parameter to an explorer collection path. */
function withNamespace(path: string, namespace?: string): string {
  return namespace ? `${path}?namespace=${encodeURIComponent(namespace)}` : path
}

/**
 * The live Kubernetes cluster explorer adapter. Every method maps directly onto a
 * `platforms-kubernetes.md` route; list responses are `{ items }` envelopes, so they are
 * unwrapped here. Errors propagate as {@link ApiRequestError} so the caller can degrade an
 * eligibility failure (404/409) to an "unavailable" view.
 */
class ApiKubernetesExplorer implements KubernetesExplorer {
  async summary(platformId: string): Promise<KubernetesClusterSummary> {
    return apiRequest<KubernetesClusterSummary>(kubernetesBase(platformId))
  }

  async listNodes(platformId: string): Promise<KubernetesNode[]> {
    const body = await apiRequest<{ items: KubernetesNode[] }>(`${kubernetesBase(platformId)}/nodes`)
    return body.items ?? []
  }

  async setNodeSchedulable(platformId: string, nodeName: string, schedulable: boolean): Promise<KubernetesNode> {
    const action = schedulable ? 'uncordon' : 'cordon'
    return apiRequest<KubernetesNode>(`${kubernetesBase(platformId)}/nodes/${encodeURIComponent(nodeName)}/${action}`, { method: 'POST' })
  }

  async listNamespaces(platformId: string): Promise<KubernetesNamespace[]> {
    const body = await apiRequest<{ items: KubernetesNamespace[] }>(`${kubernetesBase(platformId)}/namespaces`)
    return body.items ?? []
  }

  async createNamespace(platformId: string, name: string): Promise<KubernetesNamespace> {
    return apiRequest<KubernetesNamespace>(`${kubernetesBase(platformId)}/namespaces`, {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
  }

  async deleteNamespace(platformId: string, name: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`${kubernetesBase(platformId)}/namespaces/${encodeURIComponent(name)}`, { method: 'DELETE' })
  }

  async listApplications(platformId: string, namespace?: string): Promise<KubernetesApplication[]> {
    const body = await apiRequest<{ items: KubernetesApplication[] }>(withNamespace(`${kubernetesBase(platformId)}/applications`, namespace))
    return body.items ?? []
  }

  async getApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): Promise<KubernetesApplication> {
    return apiRequest<KubernetesApplication>(this.applicationPath(platformId, namespace, kind, name))
  }

  async deleteApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): Promise<void> {
    await apiRequest<{ success: boolean }>(this.applicationPath(platformId, namespace, kind, name), { method: 'DELETE' })
  }

  async scaleApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string, replicas: number): Promise<KubernetesApplication> {
    return apiRequest<KubernetesApplication>(`${this.applicationPath(platformId, namespace, kind, name)}/scale`, {
      method: 'POST',
      body: JSON.stringify({ replicas }),
    })
  }

  async restartApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`${this.applicationPath(platformId, namespace, kind, name)}/restart`, { method: 'POST' })
  }

  async listPods(platformId: string, namespace?: string): Promise<KubernetesPod[]> {
    const body = await apiRequest<{ items: KubernetesPod[] }>(withNamespace(`${kubernetesBase(platformId)}/pods`, namespace))
    return body.items ?? []
  }

  async podLogs(platformId: string, namespace: string, name: string, container?: string, tailLines?: number): Promise<KubernetesPodLogs> {
    const query = new URLSearchParams()
    if (container) query.set('container', container)
    if (tailLines) query.set('tailLines', String(tailLines))
    const suffix = query.toString() ? `?${query.toString()}` : ''
    return apiRequest<KubernetesPodLogs>(`${kubernetesBase(platformId)}/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs${suffix}`)
  }

  async deletePod(platformId: string, namespace: string, name: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`${kubernetesBase(platformId)}/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, { method: 'DELETE' })
  }

  async listServices(platformId: string, namespace?: string): Promise<KubernetesService[]> {
    const body = await apiRequest<{ items: KubernetesService[] }>(withNamespace(`${kubernetesBase(platformId)}/services`, namespace))
    return body.items ?? []
  }

  async listIngresses(platformId: string, namespace?: string): Promise<KubernetesIngress[]> {
    const body = await apiRequest<{ items: KubernetesIngress[] }>(withNamespace(`${kubernetesBase(platformId)}/ingresses`, namespace))
    return body.items ?? []
  }

  async listConfigMaps(platformId: string, namespace?: string): Promise<KubernetesConfigResource[]> {
    const body = await apiRequest<{ items: KubernetesConfigResource[] }>(withNamespace(`${kubernetesBase(platformId)}/configmaps`, namespace))
    return body.items ?? []
  }

  async listSecrets(platformId: string, namespace?: string): Promise<KubernetesConfigResource[]> {
    const body = await apiRequest<{ items: KubernetesConfigResource[] }>(withNamespace(`${kubernetesBase(platformId)}/secrets`, namespace))
    return body.items ?? []
  }

  async listPersistentVolumeClaims(platformId: string, namespace?: string): Promise<KubernetesPersistentVolumeClaim[]> {
    const body = await apiRequest<{ items: KubernetesPersistentVolumeClaim[] }>(withNamespace(`${kubernetesBase(platformId)}/persistentvolumeclaims`, namespace))
    return body.items ?? []
  }

  async apply(platformId: string, manifest: string, dryRun: boolean): Promise<KubernetesApplyResult[]> {
    const body = await apiRequest<{ results: KubernetesApplyResult[] }>(`${kubernetesBase(platformId)}/apply`, {
      method: 'POST',
      body: JSON.stringify({ manifest, dryRun }),
    })
    return body.results ?? []
  }

  /** Builds the object path for one Application, encoding each segment. */
  private applicationPath(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): string {
    return `${kubernetesBase(platformId)}/applications/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`
  }
}

/**
 * The platform contract maps directly onto the domain type, so there is no reshaping here.
 * The one translation is 404 on getPlatform, which is a stale link rather than an error to
 * surface, matching how the server repository treats a missing server.
 */
export class ApiPlatformRepository implements PlatformRepository {
  /** The live Kubernetes cluster explorer, grouped so callers use one cohesive surface. */
  readonly kubernetes: KubernetesExplorer = new ApiKubernetesExplorer()

  async listPlatforms(siteId?: string): Promise<Platform[]> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<Platform[]>(`/api/v1/platforms${suffix}`)
  }

  async getPlatform(id: string): Promise<Platform | null> {
    try {
      return await apiRequest<Platform>(`/api/v1/platforms/${encodeURIComponent(id)}`)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async getSlurmDeploymentRequirement(): Promise<SlurmDeploymentRequirement> {
    return apiRequest<SlurmDeploymentRequirement>('/api/v1/platforms/deployment-requirements/slurm')
  }

  async putSlurmDeploymentRequirement(minimum: MinimumResources | null): Promise<SlurmDeploymentRequirement> {
    return apiRequest<SlurmDeploymentRequirement>('/api/v1/platforms/deployment-requirements/slurm', {
      method: 'PUT',
      body: JSON.stringify({ minimumResources: minimum }),
    })
  }

  async deployPlatform(input: DeployPlatformInput): Promise<DeployPlatformResult> {
    return apiRequest<DeployPlatformResult>('/api/v1/platforms/deploy', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }
  async uninstallPlatform(
    id: string,
    options?: UninstallPlatformOptions,
  ): Promise<DeployPlatformResult> {
    // An absent body removes platform software; releaseServers selects the direct-release shortcut.
    return apiRequest<DeployPlatformResult>(
      `/api/v1/platforms/${encodeURIComponent(id)}/uninstall`,
      options ? { method: 'POST', body: JSON.stringify(options) } : { method: 'POST' },
    )
  }

  async deletePlatform(id: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`/api/v1/platforms/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }


  async syncPlatform(id: string): Promise<MembershipReport> {
    return apiRequest<MembershipReport>(`/api/v1/platforms/${encodeURIComponent(id)}/sync`, {
      method: 'POST',
    })
  }

  async getSlurmCluster(id: string): Promise<SlurmCluster | null> {
    try {
      const cluster = await apiRequest<SlurmCluster>(
        `/api/v1/platforms/${encodeURIComponent(id)}/slurm`,
      )
      // Normalize nullable collections so the view can iterate without guards.
      return {
        controllers: cluster.controllers ?? [],
        partitions: cluster.partitions ?? [],
        nodes: (cluster.nodes ?? []).map((node) => ({
          ...node,
          partitions: node.partitions ?? [],
        })),
      }
    } catch (error) {
      // Any handled API response (non-Slurm platform, no slurmrestd integration yet,
      // slurmrestd unreachable or rejecting) means there is no live view; degrade to null so
      // the Slurm view falls back to deployment intent plus membership. Only an unexpected,
      // non-API failure propagates.
      if (error instanceof ApiRequestError) {
        return null
      }
      throw error
    }
  }
}
