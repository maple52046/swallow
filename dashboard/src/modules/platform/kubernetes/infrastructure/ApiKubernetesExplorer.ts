import type { operations } from '@/infrastructure/api/generated/openapi'
import type { JsonRequestBody, JsonResponse } from '@/infrastructure/api/contractTypes'
import { apiRequest } from '@/infrastructure/api/client'
import type { KubernetesApplication, KubernetesApplicationKind } from '@/domain/platform/kubernetes'
import type { KubernetesExplorerPort } from '../KubernetesExplorerPort'

/** Path prefix for one deployed cluster, encoding the Platform id exactly once. */
function base(platformId: string): string {
  return `/api/v1/platforms/${encodeURIComponent(platformId)}/kubernetes`
}

/** Appends the optional namespace filter accepted by Kubernetes collection routes. */
function withNamespace(path: string, namespace?: string): string {
  return namespace ? `${path}?namespace=${encodeURIComponent(namespace)}` : path
}

/**
 * Contract-typed adapter for the Kubernetes module's live cluster explorer. It intentionally lives
 * with the module: the generic Platform adapter neither knows nor exposes Kubernetes resources.
 */
export class ApiKubernetesExplorer implements KubernetesExplorerPort {
  async summary(platformId: string) {
    return apiRequest<JsonResponse<operations['getKubernetesClusterSummary'], 200>>(base(platformId))
  }

  async listNodes(platformId: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesNodes'], 200>>(`${base(platformId)}/nodes`)
    return response.items ?? []
  }

  async setNodeSchedulable(platformId: string, nodeName: string, schedulable: boolean) {
    const operation = schedulable ? 'uncordon' : 'cordon'
    return apiRequest<JsonResponse<operations['cordonKubernetesNode'], 200>>(
      `${base(platformId)}/nodes/${encodeURIComponent(nodeName)}/${operation}`,
      { method: 'POST' },
    )
  }

  async listNamespaces(platformId: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesNamespaces'], 200>>(`${base(platformId)}/namespaces`)
    return response.items ?? []
  }

  async createNamespace(platformId: string, name: string) {
    const body: JsonRequestBody<operations['createKubernetesNamespace']> = { name }
    return apiRequest<JsonResponse<operations['createKubernetesNamespace'], 201>>(`${base(platformId)}/namespaces`, {
      method: 'POST',
      body: JSON.stringify(body),
    })
  }

  async deleteNamespace(platformId: string, name: string) {
    await apiRequest<JsonResponse<operations['deleteKubernetesNamespace'], 200>>(
      `${base(platformId)}/namespaces/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    )
  }

  async listApplications(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesApplications'], 200>>(
      withNamespace(`${base(platformId)}/applications`, namespace),
    )
    return response.items ?? []
  }

  async getApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string) {
    return apiRequest<JsonResponse<operations['getKubernetesApplication'], 200>>(this.applicationPath(platformId, namespace, kind, name))
  }

  async deleteApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string) {
    await apiRequest<JsonResponse<operations['deleteKubernetesApplication'], 200>>(
      this.applicationPath(platformId, namespace, kind, name),
      { method: 'DELETE' },
    )
  }

  async scaleApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string, replicas: number) {
    const body: JsonRequestBody<operations['scaleKubernetesApplication']> = { replicas }
    return apiRequest<JsonResponse<operations['scaleKubernetesApplication'], 200>>(
      `${this.applicationPath(platformId, namespace, kind, name)}/scale`,
      { method: 'POST', body: JSON.stringify(body) },
    )
  }

  async restartApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string) {
    await apiRequest<JsonResponse<operations['restartKubernetesApplication'], 200>>(
      `${this.applicationPath(platformId, namespace, kind, name)}/restart`,
      { method: 'POST' },
    )
  }

  async listPods(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesPods'], 200>>(
      withNamespace(`${base(platformId)}/pods`, namespace),
    )
    return response.items ?? []
  }

  async podLogs(platformId: string, namespace: string, name: string, container?: string, tailLines?: number) {
    const query = new URLSearchParams()
    if (container) query.set('container', container)
    if (tailLines) query.set('tailLines', String(tailLines))
    const suffix = query.size > 0 ? `?${query.toString()}` : ''
    return apiRequest<JsonResponse<operations['getKubernetesPodLogs'], 200>>(
      `${base(platformId)}/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs${suffix}`,
    )
  }

  async deletePod(platformId: string, namespace: string, name: string) {
    await apiRequest<JsonResponse<operations['deleteKubernetesPod'], 200>>(
      `${base(platformId)}/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    )
  }

  async listServices(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesServices'], 200>>(
      withNamespace(`${base(platformId)}/services`, namespace),
    )
    return response.items ?? []
  }

  async listIngresses(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesIngresses'], 200>>(
      withNamespace(`${base(platformId)}/ingresses`, namespace),
    )
    return response.items ?? []
  }

  async listConfigMaps(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesConfigMaps'], 200>>(
      withNamespace(`${base(platformId)}/configmaps`, namespace),
    )
    return response.items ?? []
  }

  async listSecrets(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesSecrets'], 200>>(
      withNamespace(`${base(platformId)}/secrets`, namespace),
    )
    return response.items ?? []
  }

  async listPersistentVolumeClaims(platformId: string, namespace?: string) {
    const response = await apiRequest<JsonResponse<operations['listKubernetesPersistentVolumeClaims'], 200>>(
      withNamespace(`${base(platformId)}/persistentvolumeclaims`, namespace),
    )
    return response.items ?? []
  }

  async apply(platformId: string, manifest: string, dryRun: boolean) {
    const body: JsonRequestBody<operations['applyKubernetesManifest']> = { manifest, dryRun }
    const response = await apiRequest<JsonResponse<operations['applyKubernetesManifest'], 200>>(`${base(platformId)}/apply`, {
      method: 'POST',
      body: JSON.stringify(body),
    })
    return response.results ?? []
  }

  private applicationPath(platformId: string, namespace: string, kind: KubernetesApplication['kind'], name: string): string {
    return `${base(platformId)}/applications/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`
  }
}
