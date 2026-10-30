import type { Paginated as OperationPage } from '@/application/ports/OperationRepository'
import type { Paginated as ServerPage } from '@/application/ports/ServerRepository'
import type { ApiKey, CreatedApiKey, GeneratedAccessKey, SSHKey } from '@/domain/access/types'
import type { GroupingResource, ServerPlacement } from '@/domain/infrastructure/types'
import type { AcknowledgeAlertResult, MonitoringAlert, ServerMetricsResult } from '@/domain/monitoring/types'
import type { Operation, OperationArtifact, OperationEvents, OperationTimelineEvent } from '@/domain/operation/types'
import type { Overview } from '@/domain/overview/types'
import type {
  KubernetesApplication,
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
import type {
  ConfigureGPUOperatorResult,
  DeployPlatformResult,
  GPUOperatorRelease,
  MembershipReport,
  Platform,
  SlurmCluster,
  SlurmDeploymentRequirement,
} from '@/domain/platform/types'
import type {
  BootISO,
  BootISOCatalog,
  DeploymentTargetPreflightResult,
  DeploymentTemplate,
  DeployServersResult,
  HostEnrollmentBundle,
  NetworkInspectionResult,
  NetworkTarget,
  ProvisionerHttpProxy,
  ProvisionerSubnet,
  ProvisioningOperationReference,
  ProvisioningTask,
  ServerTagOption,
  ServerTagsResult,
  VirtualMachineEnrollmentAccepted,
} from '@/domain/provisioning/types'
import type {
  BootMediaProbe,
  HypervisorVirtualMachines,
  PowerStateResult,
  ProviderEvents,
  ProvisionerDetail,
  ProvisioningActionResult,
  Server,
  ServerBootMedia,
  SetServerBootMediaResult,
  SetServerDefaultUserResult,
} from '@/domain/server/types'
import type { Integration, OSImage, Site } from '@/domain/site/types'
import type {
  DockerContainer,
  DockerContainerCreated,
  DockerEngineSummary,
  DockerImage,
  DockerImagePullResult,
  DockerNetwork,
  DockerVolume,
  RegistryCredential,
} from '@/domain/software/docker'
import type { SoftwareAssignment, SoftwareCatalogEntry, SoftwareOperationReference } from '@/domain/software/types'
import type { ItemsEnvelope as DockerItemsEnvelope } from './ApiDockerHostRepository'
import type { MetricRangeDto } from './ApiMonitoringRepository'
import type { PowerConfigurationDTO } from './ApiServerRepository'
import type { ItemsEnvelope as SoftwareItemsEnvelope } from './ApiSoftwareRepository'
import type { operations } from './generated/openapi'
import type { LoginResponse, MeResponse } from './types'

/**
 * The `application/json` body of one success status of a generated contract operation, picked by
 * operationId (`operations['…']`) rather than by path so a moved route keeps its entry.
 *
 * The constraint makes a wrong status, or a status whose body is not JSON (a 204, plain text), a
 * compile error at the entry instead of a silently vacuous check.
 */
type JsonResponse<
  Op extends { responses: Record<Status, { content: { 'application/json': unknown } }> },
  Status extends number,
> = Op['responses'][Status]['content']['application/json']

/**
 * `true` when every value the contract allows for `Wire` is also a value of the dashboard type
 * `Read`, so the adapter can hand the parsed body on as `Read`. Extra wire properties are fine (the
 * dashboard ignores them); a wire `null`, a missing required field, or a wider enum is not.
 *
 * The tuple wrapping stops the check from distributing over unions, and a `never` wire type counts
 * as unreadable so a broken lookup can never pass vacuously.
 */
type Readable<Wire, Read> = [Wire] extends [never] ? false : [Wire] extends [Read] ? true : false

/** Fails the build unless the pair is readable. */
type Assert<T extends true> = T

/** Fails the build once a known mismatch becomes readable, forcing its entry to move. */
type AssertNot<T extends false> = T

/**
 * Compile-time proof that each api-server contract response can be read as the dashboard type its
 * adapter declares. The dashboard's domain types are the wire shapes (see `types.ts`), and every
 * adapter in this folder types the parsed body with `apiRequest<T>` / `apiUpload<T>`; nothing checks
 * that `T` at runtime, so this tuple is where the provider-owned OpenAPI document
 * (`generated/openapi.ts`, from `npm run contract:generate`) and those `T`s meet.
 *
 * - One entry per adapter call that reads a JSON body, grouped by adapter, naming the adapter
 *   method and route. A method that reaches several operations (Zone/Pool, cordon/uncordon, the
 *   Server actions) has one entry per operation. Each entry uses the status the operation returns
 *   on success and the exact type argument of the call, including list envelopes.
 * - `{ success: boolean }` bodies are covered even though their adapters discard them: the type
 *   argument is still a claim about the wire.
 * - Not covered: `apiRequest<void>` calls (204s and ignored bodies), untyped `apiRequest(…)` calls
 *   whose result is discarded (there is no `T` to check), and `apiRequestText` plain-text logs.
 *   The Session refresh (`session.ts`, raw `fetch`) and the Server event stream
 *   (`ApiServerEventStream.ts`, `EventSource`) do not go through these helpers.
 *
 * A failing entry means the contract and the dashboard type disagree. Fix the dashboard type when
 * it is wrong, or raise the difference against the provider contract when the contract is; never
 * loosen the entry (`any`, `as`, `Partial`, `Pick`, `Omit`) to make it pass, and move it to
 * {@link KnownContractMismatches} only with a comment saying what differs and why it stays.
 *
 * Adding or changing an adapter call means adding or changing its entry. The file contains only
 * types, so it is erased at build time and has no runtime effect.
 */
export type ContractCompatibility = [
  // ApiApiKeyRepository
  // listApiKeys: GET /api/v1/api-keys
  Assert<Readable<JsonResponse<operations['listApiKeys'], 200>, ApiKey[]>>,
  // createApiKey: POST /api/v1/api-keys
  Assert<Readable<JsonResponse<operations['createApiKey'], 201>, CreatedApiKey>>,

  // authApi
  // login: POST /api/v1/auth/login
  Assert<Readable<JsonResponse<operations['login'], 200>, LoginResponse>>,
  // getMe: GET /api/v1/auth/me
  Assert<Readable<JsonResponse<operations['getCurrentUser'], 200>, MeResponse>>,

  // ApiDockerHostRepository
  // summary: GET /api/v1/servers/{id}/docker
  Assert<Readable<JsonResponse<operations['getDockerEngineSummary'], 200>, DockerEngineSummary>>,
  // listImages: GET /api/v1/servers/{id}/docker/images
  Assert<Readable<JsonResponse<operations['listDockerImages'], 200>, DockerItemsEnvelope<DockerImage>>>,
  // pullImage: POST /api/v1/servers/{id}/docker/images/pull
  Assert<Readable<JsonResponse<operations['pullDockerImage'], 200>, DockerImagePullResult>>,
  // listContainers: GET /api/v1/servers/{id}/docker/containers
  Assert<Readable<JsonResponse<operations['listDockerContainers'], 200>, DockerItemsEnvelope<DockerContainer>>>,
  // createContainer: POST /api/v1/servers/{id}/docker/containers
  Assert<Readable<JsonResponse<operations['createDockerContainer'], 201>, DockerContainerCreated>>,
  // containerLogs: GET /api/v1/servers/{id}/docker/containers/{containerId}/logs
  Assert<Readable<JsonResponse<operations['getDockerContainerLogs'], 200>, { logs: string }>>,
  // listVolumes: GET /api/v1/servers/{id}/docker/volumes
  Assert<Readable<JsonResponse<operations['listDockerVolumes'], 200>, DockerItemsEnvelope<DockerVolume>>>,
  // createVolume: POST /api/v1/servers/{id}/docker/volumes
  Assert<Readable<JsonResponse<operations['createDockerVolume'], 201>, DockerVolume>>,
  // listNetworks: GET /api/v1/servers/{id}/docker/networks
  Assert<Readable<JsonResponse<operations['listDockerNetworks'], 200>, DockerItemsEnvelope<DockerNetwork>>>,
  // createNetwork: POST /api/v1/servers/{id}/docker/networks
  Assert<Readable<JsonResponse<operations['createDockerNetwork'], 201>, DockerNetwork>>,

  // ApiInfrastructureRepository
  // listGroups(zone): GET /api/v1/infrastructure/zones
  Assert<Readable<JsonResponse<operations['listZones'], 200>, GroupingResource[]>>,
  // listGroups(pool): GET /api/v1/infrastructure/pools
  Assert<Readable<JsonResponse<operations['listPools'], 200>, GroupingResource[]>>,
  // createGroup(zone): POST /api/v1/infrastructure/zones
  Assert<Readable<JsonResponse<operations['createZone'], 201>, GroupingResource>>,
  // createGroup(pool): POST /api/v1/infrastructure/pools
  Assert<Readable<JsonResponse<operations['createPool'], 201>, GroupingResource>>,
  // updateGroup(zone): PATCH /api/v1/infrastructure/zones/{id}
  Assert<Readable<JsonResponse<operations['updateZone'], 200>, GroupingResource>>,
  // updateGroup(pool): PATCH /api/v1/infrastructure/pools/{id}
  Assert<Readable<JsonResponse<operations['updatePool'], 200>, GroupingResource>>,
  // assignServerPlacement: PUT /api/v1/servers/{id}/placement
  Assert<Readable<JsonResponse<operations['assignServerPlacement'], 200>, ServerPlacement>>,

  // ApiMonitoringRepository
  // listAlerts: GET /api/v1/monitoring/alerts
  Assert<Readable<JsonResponse<operations['listAlerts'], 200>, MonitoringAlert[]>>,
  // acknowledgeAlert: POST /api/v1/monitoring/alerts/{fingerprint}/acknowledge
  Assert<Readable<JsonResponse<operations['acknowledgeAlert'], 200>, AcknowledgeAlertResult>>,
  // getServerMetrics: GET /api/v1/monitoring/metrics
  Assert<Readable<JsonResponse<operations['getMetricValues'], 200>, ServerMetricsResult>>,
  // getMetricRange: GET /api/v1/monitoring/metrics/range (read as the adapter's DTO, then mapped)
  Assert<Readable<JsonResponse<operations['getMetricRange'], 200>, MetricRangeDto>>,

  // ApiOperationRepository (listOperations, getOperation, retryOperation, rerunOperation: see
  // KnownContractMismatches)
  // getEvents: GET /api/v1/operations/{id}/events (deprecated alias)
  Assert<Readable<JsonResponse<operations['getOperationEventsViaDeprecatedAlias'], 200>, OperationEvents>>,
  // getTimeline: GET /api/v1/workflows/{id}/timeline
  Assert<Readable<JsonResponse<operations['getWorkflowTimeline'], 200>, OperationTimelineEvent[] | null>>,
  // getStepEvents: GET /api/v1/workflows/{id}/tasks/{taskId}/events
  Assert<Readable<JsonResponse<operations['getWorkflowTaskEvents'], 200>, OperationEvents>>,
  // getStepArtifacts: GET /api/v1/workflows/{id}/tasks/{taskId}/artifacts
  Assert<Readable<JsonResponse<operations['listWorkflowTaskArtifacts'], 200>, OperationArtifact[] | null>>,

  // ApiOverviewRepository (getOverview: see KnownContractMismatches)

  // ApiPlatformRepository: Kubernetes explorer
  // summary: GET /api/v1/platforms/{id}/kubernetes
  Assert<Readable<JsonResponse<operations['getKubernetesClusterSummary'], 200>, KubernetesClusterSummary>>,
  // listNodes: GET /api/v1/platforms/{id}/kubernetes/nodes
  Assert<Readable<JsonResponse<operations['listKubernetesNodes'], 200>, { items: KubernetesNode[] }>>,
  // setNodeSchedulable(false): POST /api/v1/platforms/{id}/kubernetes/nodes/{node}/cordon
  Assert<Readable<JsonResponse<operations['cordonKubernetesNode'], 200>, KubernetesNode>>,
  // setNodeSchedulable(true): POST /api/v1/platforms/{id}/kubernetes/nodes/{node}/uncordon
  Assert<Readable<JsonResponse<operations['uncordonKubernetesNode'], 200>, KubernetesNode>>,
  // listNamespaces: GET /api/v1/platforms/{id}/kubernetes/namespaces
  Assert<Readable<JsonResponse<operations['listKubernetesNamespaces'], 200>, { items: KubernetesNamespace[] }>>,
  // createNamespace: POST /api/v1/platforms/{id}/kubernetes/namespaces
  Assert<Readable<JsonResponse<operations['createKubernetesNamespace'], 201>, KubernetesNamespace>>,
  // deleteNamespace: DELETE /api/v1/platforms/{id}/kubernetes/namespaces/{namespace}
  Assert<Readable<JsonResponse<operations['deleteKubernetesNamespace'], 200>, { success: boolean }>>,
  // listApplications: GET /api/v1/platforms/{id}/kubernetes/applications
  Assert<Readable<JsonResponse<operations['listKubernetesApplications'], 200>, { items: KubernetesApplication[] }>>,
  // getApplication: GET /api/v1/platforms/{id}/kubernetes/applications/{namespace}/{kind}/{name}
  Assert<Readable<JsonResponse<operations['getKubernetesApplication'], 200>, KubernetesApplication>>,
  // deleteApplication: DELETE /api/v1/platforms/{id}/kubernetes/applications/{namespace}/{kind}/{name}
  Assert<Readable<JsonResponse<operations['deleteKubernetesApplication'], 200>, { success: boolean }>>,
  // scaleApplication: POST /api/v1/platforms/{id}/kubernetes/applications/{namespace}/{kind}/{name}/scale
  Assert<Readable<JsonResponse<operations['scaleKubernetesApplication'], 200>, KubernetesApplication>>,
  // restartApplication: POST /api/v1/platforms/{id}/kubernetes/applications/{namespace}/{kind}/{name}/restart
  Assert<Readable<JsonResponse<operations['restartKubernetesApplication'], 200>, { success: boolean }>>,
  // listPods: GET /api/v1/platforms/{id}/kubernetes/pods
  Assert<Readable<JsonResponse<operations['listKubernetesPods'], 200>, { items: KubernetesPod[] }>>,
  // podLogs: GET /api/v1/platforms/{id}/kubernetes/pods/{namespace}/{name}/logs
  Assert<Readable<JsonResponse<operations['getKubernetesPodLogs'], 200>, KubernetesPodLogs>>,
  // deletePod: DELETE /api/v1/platforms/{id}/kubernetes/pods/{namespace}/{name}
  Assert<Readable<JsonResponse<operations['deleteKubernetesPod'], 200>, { success: boolean }>>,
  // listServices: GET /api/v1/platforms/{id}/kubernetes/services
  Assert<Readable<JsonResponse<operations['listKubernetesServices'], 200>, { items: KubernetesService[] }>>,
  // listIngresses: GET /api/v1/platforms/{id}/kubernetes/ingresses
  Assert<Readable<JsonResponse<operations['listKubernetesIngresses'], 200>, { items: KubernetesIngress[] }>>,
  // listConfigMaps: GET /api/v1/platforms/{id}/kubernetes/configmaps
  Assert<Readable<JsonResponse<operations['listKubernetesConfigMaps'], 200>, { items: KubernetesConfigResource[] }>>,
  // listSecrets: GET /api/v1/platforms/{id}/kubernetes/secrets
  Assert<Readable<JsonResponse<operations['listKubernetesSecrets'], 200>, { items: KubernetesConfigResource[] }>>,
  // listPersistentVolumeClaims: GET /api/v1/platforms/{id}/kubernetes/persistentvolumeclaims
  Assert<Readable<JsonResponse<operations['listKubernetesPersistentVolumeClaims'], 200>, { items: KubernetesPersistentVolumeClaim[] }>>,
  // apply: POST /api/v1/platforms/{id}/kubernetes/apply
  Assert<Readable<JsonResponse<operations['applyKubernetesManifest'], 200>, { results: KubernetesApplyResult[] }>>,

  // ApiPlatformRepository
  // listPlatforms: GET /api/v1/platforms
  Assert<Readable<JsonResponse<operations['listPlatforms'], 200>, Platform[]>>,
  // getPlatform: GET /api/v1/platforms/{id}
  Assert<Readable<JsonResponse<operations['getPlatform'], 200>, Platform>>,
  // getSlurmDeploymentRequirement: GET /api/v1/platforms/deployment-requirements/slurm
  Assert<Readable<JsonResponse<operations['getSlurmDeploymentRequirement'], 200>, SlurmDeploymentRequirement>>,
  // putSlurmDeploymentRequirement: PUT /api/v1/platforms/deployment-requirements/slurm
  Assert<Readable<JsonResponse<operations['replaceSlurmDeploymentRequirement'], 200>, SlurmDeploymentRequirement>>,
  // listGPUOperatorReleases: GET /api/v1/platforms/gpu-operator/releases
  Assert<Readable<JsonResponse<operations['listGPUOperatorReleases'], 200>, { items: GPUOperatorRelease[] }>>,
  // configureGPUOperator: PUT /api/v1/platforms/{id}/gpu-operator
  Assert<Readable<JsonResponse<operations['configureGPUOperator'], 202>, ConfigureGPUOperatorResult>>,
  // deployPlatform: POST /api/v1/platforms/deploy
  Assert<Readable<JsonResponse<operations['deployPlatform'], 202>, DeployPlatformResult>>,
  // uninstallPlatform: POST /api/v1/platforms/{id}/uninstall
  Assert<Readable<JsonResponse<operations['uninstallPlatform'], 202>, DeployPlatformResult>>,
  // deletePlatform: DELETE /api/v1/platforms/{id}
  Assert<Readable<JsonResponse<operations['deletePlatform'], 200>, { success: boolean }>>,
  // syncPlatform: POST /api/v1/platforms/{id}/sync
  Assert<Readable<JsonResponse<operations['syncPlatformMembership'], 200>, MembershipReport>>,
  // getSlurmCluster: GET /api/v1/platforms/{id}/slurm
  Assert<Readable<JsonResponse<operations['getSlurmClusterState'], 200>, SlurmCluster>>,

  // ApiProvisioningRepository (deployServers: see KnownContractMismatches)
  // listTemplates: GET /api/v1/provisioning/templates
  Assert<Readable<JsonResponse<operations['listDeploymentTemplates'], 200>, DeploymentTemplate[]>>,
  // getTemplate: GET /api/v1/provisioning/templates/{id}
  Assert<Readable<JsonResponse<operations['getDeploymentTemplate'], 200>, DeploymentTemplate>>,
  // createTemplate: POST /api/v1/provisioning/templates
  Assert<Readable<JsonResponse<operations['createDeploymentTemplate'], 201>, DeploymentTemplate>>,
  // updateTemplate: PATCH /api/v1/provisioning/templates/{id}
  Assert<Readable<JsonResponse<operations['updateDeploymentTemplate'], 200>, DeploymentTemplate>>,
  // preflightDeploymentTargets: POST /api/v1/provisioning/deployments/preflight
  Assert<Readable<JsonResponse<operations['preflightOsDeploymentTargets'], 200>, DeploymentTargetPreflightResult>>,
  // inspectDeploymentNetworks: POST /api/v1/provisioning/networks/inspect
  Assert<Readable<JsonResponse<operations['inspectDeploymentNetworks'], 200>, NetworkInspectionResult>>,
  // createDeploymentOperation: POST /api/v1/provisioning/deployment-operations
  Assert<Readable<JsonResponse<operations['createDeploymentOperation'], 202>, ProvisioningOperationReference>>,
  // createReleaseOperation: POST /api/v1/provisioning/release-operations
  Assert<Readable<JsonResponse<operations['createReleaseOperation'], 202>, ProvisioningOperationReference>>,
  // createRecoverOperation: POST /api/v1/provisioning/recover-operations
  Assert<Readable<JsonResponse<operations['createRecoverOperation'], 202>, ProvisioningOperationReference>>,
  // createImageVerification: POST /api/v1/provisioning/image-verifications
  Assert<Readable<JsonResponse<operations['createOsImageVerification'], 202>, ProvisioningOperationReference>>,
  // listOSImages: GET /api/v1/provisioning/images
  Assert<Readable<JsonResponse<operations['listOsImages'], 200>, OSImage[]>>,
  // uploadOSImage (apiUpload): POST /api/v1/provisioning/images
  Assert<Readable<JsonResponse<operations['uploadOsImage'], 201>, OSImage>>,
  // listServerTags: GET /api/v1/provisioning/tags
  Assert<Readable<JsonResponse<operations['listServerTags'], 200>, { tags: ServerTagOption[] }>>,
  // editServerTags: POST /api/v1/provisioning/tags
  Assert<Readable<JsonResponse<operations['editServerTags'], 200>, { servers: ServerTagsResult[] }>>,
  // listBootISOs: GET /api/v1/provisioning/boot-isos
  Assert<Readable<JsonResponse<operations['listBootIsos'], 200>, BootISOCatalog>>,
  // createBootISO: POST /api/v1/provisioning/boot-isos
  Assert<Readable<JsonResponse<operations['createBootIso'], 201>, BootISO>>,
  // rebuildBootISO: POST /api/v1/provisioning/boot-isos/{id}/rebuild
  Assert<Readable<JsonResponse<operations['rebuildBootIso'], 200>, BootISO>>,
  // getHostEnrollmentBundle: POST /api/v1/provisioning/integrations/{id}/enroll-bundle
  Assert<Readable<JsonResponse<operations['createServerEnrollmentBundle'], 200>, HostEnrollmentBundle>>,
  // enrollVirtualMachines: POST /api/v1/provisioning/virtual-machine-enrollments
  Assert<Readable<JsonResponse<operations['createVirtualMachineEnrollment'], 202>, VirtualMachineEnrollmentAccepted>>,
  // getProvisionerHttpProxy: GET /api/v1/provisioning/integrations/{id}/http-proxy
  Assert<Readable<JsonResponse<operations['getProvisionerHttpProxy'], 200>, ProvisionerHttpProxy>>,
  // setProvisionerHttpProxy: PUT /api/v1/provisioning/integrations/{id}/http-proxy
  Assert<Readable<JsonResponse<operations['replaceProvisionerHttpProxy'], 200>, ProvisionerHttpProxy>>,
  // listProvisionerSubnets: GET /api/v1/provisioning/integrations/{id}/subnets
  Assert<Readable<JsonResponse<operations['listProvisionerSubnets'], 200>, { integrationId: string; subnets: ProvisionerSubnet[] }>>,
  // setProvisionerSubnetDnsServers: PUT /api/v1/provisioning/integrations/{id}/subnets/{subnetId}/dns-servers
  Assert<Readable<JsonResponse<operations['replaceSubnetDnsServers'], 200>, ProvisionerSubnet>>,

  // ApiRegistryCredentialRepository
  // list: GET /api/v1/software/docker-ce/registry-credentials
  Assert<Readable<JsonResponse<operations['listRegistryCredentials'], 200>, { items: RegistryCredential[] }>>,
  // create: POST /api/v1/software/docker-ce/registry-credentials
  Assert<Readable<JsonResponse<operations['createRegistryCredential'], 201>, RegistryCredential>>,
  // replace: PUT /api/v1/software/docker-ce/registry-credentials/{credentialId}
  Assert<Readable<JsonResponse<operations['replaceRegistryCredential'], 200>, RegistryCredential>>,

  // ApiServerRepository (getProvisionerDetail, setDefaultUser: see KnownContractMismatches)
  // listServers: GET /api/v1/servers
  Assert<Readable<JsonResponse<operations['listServers'], 200>, ServerPage<Server>>>,
  // getServer: GET /api/v1/servers/{id}
  Assert<Readable<JsonResponse<operations['getServer'], 200>, Server>>,
  // deployServer: POST /api/v1/servers/{id}/deploy (deprecated)
  Assert<Readable<JsonResponse<operations['deployServer'], 202>, ProvisioningActionResult>>,
  // releaseServer: POST /api/v1/servers/{id}/release (deprecated)
  Assert<Readable<JsonResponse<operations['releaseServer'], 202>, ProvisioningActionResult>>,
  // refreshServer: POST /api/v1/servers/{id}/refresh
  Assert<Readable<JsonResponse<operations['refreshServer'], 200>, ProvisioningActionResult>>,
  // getProviderEvents: GET /api/v1/servers/{id}/events
  Assert<Readable<JsonResponse<operations['listServerProviderEvents'], 200>, ProviderEvents>>,
  // getNetwork: GET /api/v1/servers/{id}/network
  Assert<Readable<JsonResponse<operations['getServerNetwork'], 200>, NetworkTarget>>,
  // createNetworkLink: POST /api/v1/servers/{id}/network/interfaces/{interfaceId}/links
  Assert<Readable<JsonResponse<operations['createServerNetworkLink'], 200>, NetworkTarget>>,
  // replaceNetworkLink: PUT /api/v1/servers/{id}/network/interfaces/{interfaceId}/links/{linkId}
  Assert<Readable<JsonResponse<operations['replaceServerNetworkLink'], 200>, NetworkTarget>>,
  // deleteNetworkLink: DELETE /api/v1/servers/{id}/network/interfaces/{interfaceId}/links/{linkId}
  Assert<Readable<JsonResponse<operations['deleteServerNetworkLink'], 200>, NetworkTarget>>,
  // listProvisioningTasks: GET /api/v1/servers/{id}/provisioning-tasks
  Assert<Readable<JsonResponse<operations['listServerProvisioningTasks'], 200>, ProvisioningTask[]>>,
  // getProvisioningTask: GET /api/v1/provisioning/tasks/{id}
  Assert<Readable<JsonResponse<operations['getProvisioningTask'], 200>, ProvisioningTask>>,
  // retryProvisioningTask: POST /api/v1/provisioning/tasks/{id}/retry
  Assert<Readable<JsonResponse<operations['retryProvisioningTask'], 202>, ProvisioningTask>>,
  // runServerAction: POST /api/v1/servers/{id}/{action}, one entry per ServerAction route. The
  // union's `recover` has no such route; every caller sends it to createRecoverOperation instead.
  // runServerAction('power-on')
  Assert<Readable<JsonResponse<operations['powerOnServer'], 202>, ProvisioningActionResult>>,
  // runServerAction('power-off')
  Assert<Readable<JsonResponse<operations['powerOffServer'], 202>, ProvisioningActionResult>>,
  // runServerAction('inspect')
  Assert<Readable<JsonResponse<operations['inspectServer'], 202>, ProvisioningActionResult>>,
  // runServerAction('test')
  Assert<Readable<JsonResponse<operations['testServer'], 202>, ProvisioningActionResult>>,
  // runServerAction('abort')
  Assert<Readable<JsonResponse<operations['abortServerLifecycleAction'], 202>, ProvisioningActionResult>>,
  // runServerAction('override-failed-testing')
  Assert<Readable<JsonResponse<operations['overrideServerFailedTesting'], 202>, ProvisioningActionResult>>,
  // runServerAction('lock')
  Assert<Readable<JsonResponse<operations['lockServer'], 202>, ProvisioningActionResult>>,
  // runServerAction('unlock')
  Assert<Readable<JsonResponse<operations['unlockServer'], 202>, ProvisioningActionResult>>,
  // runServerAction('mark-broken')
  Assert<Readable<JsonResponse<operations['markServerBroken'], 202>, ProvisioningActionResult>>,
  // runServerAction('mark-fixed')
  Assert<Readable<JsonResponse<operations['markServerFixed'], 202>, ProvisioningActionResult>>,
  // runServerAction('rescue-mode')
  Assert<Readable<JsonResponse<operations['enterServerRescueMode'], 202>, ProvisioningActionResult>>,
  // runServerAction('exit-rescue-mode')
  Assert<Readable<JsonResponse<operations['exitServerRescueMode'], 202>, ProvisioningActionResult>>,
  // queryPowerState: GET /api/v1/servers/{id}/power-state
  Assert<Readable<JsonResponse<operations['getServerPowerState'], 200>, PowerStateResult>>,
  // getPowerConfiguration: GET /api/v1/servers/{id}/power-configuration (read as the DTO, then narrowed)
  Assert<Readable<JsonResponse<operations['getServerPowerConfiguration'], 200>, PowerConfigurationDTO>>,
  // setPowerConfiguration: PUT /api/v1/servers/{id}/power-configuration (read as the DTO, then narrowed)
  Assert<Readable<JsonResponse<operations['setServerPowerConfiguration'], 200>, PowerConfigurationDTO>>,
  // getBootMedia: GET /api/v1/servers/{id}/boot-media
  Assert<Readable<JsonResponse<operations['getServerBootMedia'], 200>, ServerBootMedia>>,
  // setBootMedia: PUT /api/v1/servers/{id}/boot-media
  Assert<Readable<JsonResponse<operations['setServerBootMedia'], 200>, SetServerBootMediaResult>>,
  // probeBootMedia: POST /api/v1/servers/{id}/redfish/probe. The adapter reads Partial<BootMediaProbe>
  // to tolerate an older API that omits a half; the contract always sends both, so the full type
  // is asserted, which implies the adapter's.
  Assert<Readable<JsonResponse<operations['probeServerBootMedia'], 200>, BootMediaProbe>>,
  // listVirtualMachines: GET /api/v1/servers/{id}/virtual-machines
  Assert<Readable<JsonResponse<operations['listHypervisorVirtualMachines'], 200>, HypervisorVirtualMachines>>,

  // ApiSiteRepository
  // listSites: GET /api/v1/sites
  Assert<Readable<JsonResponse<operations['listSites'], 200>, Site[]>>,
  // createSite: POST /api/v1/sites
  Assert<Readable<JsonResponse<operations['createSite'], 201>, Site>>,
  // updateSite: PATCH /api/v1/sites/{id}
  Assert<Readable<JsonResponse<operations['updateSite'], 200>, Site>>,
  // deleteSite: DELETE /api/v1/sites/{id}
  Assert<Readable<JsonResponse<operations['deleteSite'], 200>, { success: boolean }>>,
  // listIntegrations: GET /api/v1/integrations
  Assert<Readable<JsonResponse<operations['listIntegrations'], 200>, Integration[]>>,
  // getIntegration: GET /api/v1/integrations/{id}
  Assert<Readable<JsonResponse<operations['getIntegration'], 200>, Integration>>,
  // createIntegration: POST /api/v1/integrations
  Assert<Readable<JsonResponse<operations['createIntegration'], 201>, Integration>>,
  // updateIntegration: PATCH /api/v1/integrations/{id}
  Assert<Readable<JsonResponse<operations['updateIntegration'], 200>, Integration>>,
  // replaceIntegrationCredential: PUT /api/v1/integrations/{id}/credential
  Assert<Readable<JsonResponse<operations['replaceIntegrationCredential'], 200>, { success: boolean }>>,
  // deleteIntegration: DELETE /api/v1/integrations/{id}
  Assert<Readable<JsonResponse<operations['deleteIntegration'], 200>, { success: boolean }>>,
  // listOSImages: GET /api/v1/provisioning/images
  Assert<Readable<JsonResponse<operations['listOsImages'], 200>, OSImage[]>>,

  // ApiSoftwareRepository
  // listCatalog: GET /api/v1/software/catalog
  Assert<Readable<JsonResponse<operations['getSoftwareCatalog'], 200>, SoftwareItemsEnvelope<SoftwareCatalogEntry>>>,
  // listAssignments: GET /api/v1/software/assignments
  Assert<Readable<JsonResponse<operations['listSoftwareAssignments'], 200>, SoftwareItemsEnvelope<SoftwareAssignment>>>,
  // installSoftware: POST /api/v1/software/assignments
  Assert<Readable<JsonResponse<operations['installSoftware'], 202>, SoftwareOperationReference>>,
  // uninstallSoftware: POST /api/v1/software/uninstall
  Assert<Readable<JsonResponse<operations['uninstallSoftware'], 202>, SoftwareOperationReference>>,

  // ApiSSHKeyRepository
  // listSSHKeys: GET /api/v1/ssh-keys
  Assert<Readable<JsonResponse<operations['listSshKeys'], 200>, SSHKey[]>>,
  // getSSHKey: GET /api/v1/ssh-keys/{keyId}
  Assert<Readable<JsonResponse<operations['getSshKey'], 200>, SSHKey>>,
  // importAccessKey: POST /api/v1/ssh-keys
  Assert<Readable<JsonResponse<operations['importSshKey'], 201>, SSHKey>>,
  // generateAccessKey: POST /api/v1/ssh-keys/generate
  Assert<Readable<JsonResponse<operations['generateSshKey'], 201>, GeneratedAccessKey>>,
  // replaceDeploymentKey: PUT /api/v1/ssh-keys/deployment
  Assert<Readable<JsonResponse<operations['replaceDeploymentKey'], 200>, SSHKey>>,
  // regenerateDeploymentKey: POST /api/v1/ssh-keys/deployment/regenerate
  Assert<Readable<JsonResponse<operations['regenerateDeploymentKey'], 200>, SSHKey>>,
]

/**
 * Adapter calls whose contract response cannot yet be read as the declared dashboard type, each
 * kept here, rather than in {@link ContractCompatibility}, with a comment saying exactly what
 * differs and why the dashboard type was not simply corrected.
 *
 * The entries assert `false`, so this tuple is a ratchet: when the contract or the dashboard type
 * changes so that a pair becomes readable, the build fails here, and the entry must move into
 * `ContractCompatibility`, where it then stays enforced. A pair must not be added here to silence a
 * new failure without the same explanation, and a pair here must not be "fixed" by loosening it.
 *
 * Like the rest of the file this is type-only and has no runtime effect; the runtime behavior each
 * comment describes is the adapter's or the presentation's, unchanged by these checks.
 */
export type KnownContractMismatches = [
  // ApiOperationRepository: Workflow projections. The contract sends `null` for an empty
  // collection (Go nil slices, and records written before collection normalization), and the
  // adapter's normalizeOperation turns every such null into `[]` before an Operation leaves the
  // adapter, so nothing null reaches the UI. The call's type argument is that normalized domain
  // Operation, not the wire shape. Making the domain fields nullable would force changes in
  // consumers that rely on the arrays (domain releasesServer, OperatorOperationsPage, the detail
  // pages); the fix is to read the wire type (for example the generated one) and give it to
  // normalizeOperation as its input.
  // listOperations: GET /api/v1/workflows. Each item: `targetServerIds`, `steps`, and
  // `steps[].dependsOn` are `T[] | null` on the wire, non-null `T[]` (or `T[] | undefined`) here;
  // `targetResources` and `leases` are `T[] | null`, `T[] | undefined` here.
  AssertNot<Readable<JsonResponse<operations['listWorkflows'], 200>, OperationPage<Operation>>>,
  // getOperation: GET /api/v1/workflows/{id}. Same five fields as listOperations.
  AssertNot<Readable<JsonResponse<operations['getWorkflow'], 200>, Operation>>,
  // retryOperation: POST /api/v1/operations/{id}/retry (deprecated alias, LegacyOperation).
  // `targetServerIds`, `steps`, `steps[].dependsOn` are `T[] | null` on the wire.
  AssertNot<Readable<JsonResponse<operations['retryOperationViaDeprecatedAlias'], 202>, Operation>>,
  // rerunOperation: POST /api/v1/workflows/{id}/rerun. Same five fields as listOperations.
  AssertNot<Readable<JsonResponse<operations['rerunWorkflow'], 202>, Operation>>,

  // ApiOverviewRepository
  // getOverview: GET /api/v1/overview. `operations.recent[].targetServerIds` is `string[] | null`
  // on the wire (a Workflow with no target Server) and `string[]` here, because `recent` reuses the
  // domain Operation, and this adapter does not normalize. Harmless today: OperatorOverviewPage
  // reads only id, intent, kind, execution.status, and requestedAt. Not fixed because the field is
  // shared with Operation consumers that dereference it; the fix is a dedicated overview projection
  // type or normalizing in the adapter (a runtime change).
  AssertNot<Readable<JsonResponse<operations['getOverview'], 200>, Overview>>,

  // ApiProvisioningRepository
  // deployServers: POST /api/v1/provisioning/deployments (deprecated alias). `accepted[].powerState`
  // is a plain `string` on the wire and `'on' | 'off' | 'error' | 'unknown' | ''` here. The contract
  // inlines this item instead of using ServerProvisioningSnapshot, which types the same field as
  // ServerPowerState for the same server-side struct, so the contract should be tightened rather
  // than the shared ProvisioningActionResult widened to `string`. No presentation code calls
  // deployServers.
  AssertNot<Readable<JsonResponse<operations['deployServerBatchViaDeprecatedAlias'], 202>, DeployServersResult>>,

  // ApiServerRepository
  // getProvisionerDetail: GET /api/v1/servers/{id}/provisioner-detail. `tables[].columns` is
  // `string[] | null` and `tables[].rows` is `Array<string[] | null> | null` on the wire;
  // DetailTable has `string[]` and `string[][]`. DetailTableView and ServerSummaryTab read
  // `rows.length` and `columns.map` without a guard, so null would throw; today's only provider
  // (MAAS) always sets columns and drops tables without rows, so null does not occur yet. Fixing
  // the type needs guards in those components, or a contract that promises arrays as the
  // implementation already delivers them.
  AssertNot<Readable<JsonResponse<operations['getServerProvisionerDetail'], 200>, ProvisionerDetail>>,
  // setDefaultUser: PUT /api/v1/servers/{id}/default-user. `defaultUser` is
  // `ServerDefaultUser | null` on the wire and non-null here. A successful set always has an
  // effective default user, so null does not occur on 200; defaultUserSavedToast reads
  // `result.defaultUser.user` unguarded, so marking it nullable needs a presentation change. The
  // contract is looser than the implementation here and should declare the 200 value non-null.
  AssertNot<Readable<JsonResponse<operations['setServerDefaultUser'], 200>, SetServerDefaultUserResult>>,
]
