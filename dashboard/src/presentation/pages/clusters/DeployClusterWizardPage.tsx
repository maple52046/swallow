import { useMemo, useState, type ReactNode } from 'react'
import {
  Alert,
  AlertVariant,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Label,
  LabelGroup,
  TextInput,
  Title,
  Wizard,
  WizardStep,
} from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { LockBadge } from '@/presentation/components/AxisBadge'
import { useApp } from '@/di/AppProvider'
import { clusterLifecycleLabel } from '@/domain/cluster/lifecycle'
import type { Cluster, GPUStackOwner, NodeRole, RoleAssignment } from '@/domain/cluster/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { SingleSelect } from '@/presentation/components/SingleSelect'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useDeployableServers } from './useDeployableServers'

const DEFAULT_K0S_VERSION = 'v1.36.3+k0s.2'
const DEFAULT_POD_CIDR = '10.244.0.0/16'
const DEFAULT_SERVICE_CIDR = '10.96.0.0/12'

type RoleChoice = 'none' | NodeRole
type TopologyChoice = 'standalone' | 'multi-node' | 'high-availability'

function validAddress(value: string): boolean {
  const parts = value.split('.')
  return parts.length === 4 && parts.every(
    (part) => /^\d{1,3}$/.test(part) && Number(part) <= 255,
  )
}

function validCIDR(value: string): boolean {
  const [address, prefix, ...rest] = value.split('/')
  const numericPrefix = Number(prefix)
  return rest.length === 0 && validAddress(address) && /^\d{1,2}$/.test(prefix ?? '') &&
    numericPrefix >= 0 && numericPrefix <= 32
}

/** Human labels and risk language for the three supported placement shapes. */
function topologyPresentation(topology: TopologyChoice) {
  switch (topology) {
    case 'standalone':
      return {
        label: 'Standalone',
        guidance: 'One Server runs the control plane and workloads. There is no control-plane failover, but the cluster can be expanded later.',
      }
    case 'multi-node':
      return {
        label: 'Multi-node (non-HA)',
        guidance: 'One Server runs the control plane and one or more workers run workloads. A control-plane outage stops cluster management.',
      }
    case 'high-availability':
      return {
        label: 'High availability',
        guidance: 'An odd control-plane quorum of at least three uses a virtual IP. At least one selected Server must run workloads.',
      }
  }
}

/** Suggests a network range to verify without claiming that any particular address is free. */
function selectedNetworkGuidance(addresses: string[]): string {
  const ipv4 = addresses.find(validAddress)
  if (!ipv4) return 'Selected machines have no observed IPv4 address. Verify provider inventory before deployment.'
  const octets = ipv4.split('.')
  return `Observed addresses include ${octets.slice(0, 3).join('.')}.x. Confirm routing, DHCP ranges, and address reservations with the network owner.`
}

interface ExistingClusterAssignment {
  clusterName: string
  clusterType: string
  detail: string
}

/**
 * Maps backend-owned deployment claims and observed membership into one candidate-table
 * explanation. Durable claims take precedence because they also cover partial deployments
 * whose membership has not become observable yet.
 */
function existingClusterAssignment(
  server: Server,
  clusters: Cluster[],
  deploymentClaims: Record<string, Cluster>,
): ExistingClusterAssignment | null {
  const claim = deploymentClaims[server.id]
  if (claim) {
    const role = server.membership?.clusterId === claim.id
      ? server.membership.role
      : null
    return {
      clusterName: claim.name,
      clusterType: claim.type,
      detail: role
        ? `${role} · ${clusterLifecycleLabel(claim.lifecycleState)}`
        : clusterLifecycleLabel(claim.lifecycleState),
    }
  }
  if (!server.membership) return null
  const cluster = clusters.find((candidate) => candidate.id === server.membership?.clusterId)
  return {
    clusterName: cluster?.name ?? server.membership.clusterId,
    clusterType: cluster?.type ?? 'unknown',
    detail: server.membership.role,
  }
}

/**
 * PatternFly cluster deployment workflow.
 *
 * Machine selection intentionally precedes networking: the selected topology and observed
 * addresses decide whether a VIP exists and give the operator concrete allocation context.
 * Accepted work stays on the Cluster page; Operations remains a troubleshooting drill-down.
 */
export function DeployClusterWizardPage() {
  const navigate = useNavigate()
  const { clusters } = useApp()
  const { siteId: scopedSiteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [siteId, setSiteId] = useState<string | undefined>(scopedSiteId)
  const [name, setName] = useState('')
  const [gpuStackOwner, setGPUStackOwner] = useState<GPUStackOwner>('provisioning')
  const [k0sVersion, setK0sVersion] = useState(DEFAULT_K0S_VERSION)
  const [topology, setTopology] = useState<TopologyChoice>('high-availability')
  const [apiVip, setAPIVip] = useState('')
  const [apiVipPrefix, setAPIVipPrefix] = useState('24')
  const [podCidr, setPodCidr] = useState(DEFAULT_POD_CIDR)
  const [serviceCidr, setServiceCidr] = useState(DEFAULT_SERVICE_CIDR)
  const [roles, setRoles] = useState<Record<string, RoleChoice>>({})
  const [workloadControllers, setWorkloadControllers] = useState<Record<string, boolean>>({})
  const [submitting, setSubmitting] = useState(false)
  const effectiveSiteId = siteId ?? scopedSiteId
  const state = useDeployableServers(effectiveSiteId)

  const assignments = useMemo<RoleAssignment[]>(() => Object.entries(roles)
    .filter(([, role]) => role !== 'none')
    .map(([serverId, role]) => ({
      serverId,
      role: role as NodeRole,
      ...(role === 'control-plane' && workloadControllers[serverId]
        ? { runWorkloads: true }
        : {}),
    })), [roles, workloadControllers])
  const controllers = assignments.filter((item) => item.role === 'control-plane').length
  const workers = assignments.filter((item) => item.role === 'worker').length
  const workloadCount = workers + assignments.filter(
    (item) => item.role === 'control-plane' && item.runWorkloads,
  ).length
  const basicsValid = Boolean(effectiveSiteId && name.trim() && k0sVersion.trim())
  const hasLockedAssignment = state.status === 'ready' && assignments.some((assignment) =>
    state.data.servers.find((server) => server.id === assignment.serverId)?.provisioning?.locked)
  const hasLockedServers = state.status === 'ready'
    && state.data.servers.some((server) => server.provisioning?.locked)
  const hasAssignedServers = state.status === 'ready'
    && state.data.servers.some((server) => Boolean(existingClusterAssignment(
      server,
      state.data.clusters,
      state.data.deploymentClaims,
    )))
  const topologyValid = topology === 'standalone'
    ? assignments.length === 1 && controllers === 1 && workloadCount === 1
    : topology === 'multi-node'
      ? assignments.length >= 2 && controllers === 1 && workers >= 1
      : controllers >= 3 && controllers % 2 === 1 && workloadCount >= 1
  const machinesValid = topologyValid && !hasLockedAssignment
  const networkingValid = validCIDR(podCidr.trim()) && validCIDR(serviceCidr.trim()) && (
    topology !== 'high-availability' || (
      validAddress(apiVip.trim()) && Number(apiVipPrefix) >= 1 && Number(apiVipPrefix) <= 32
    )
  )

  const selectedServers = state.status === 'ready'
    ? assignments.flatMap((assignment) => {
      const server = state.data.servers.find((candidate) => candidate.id === assignment.serverId)
      return server ? [server] : []
    })
    : []
  const selectedAddresses = selectedServers.flatMap((server) => server.addresses)
  const initialController = selectedServers.find(
    (server) => roles[server.id] === 'control-plane',
  )
  const initialControllerAddress = initialController
    ? serverPrimaryAddress(initialController)
    : null

  const changeTopology = (next: TopologyChoice) => {
    setTopology(next)
    setRoles({})
    setWorkloadControllers({})
    setAPIVip('')
  }

  const changeRole = (serverId: string, next: RoleChoice) => {
    const server = state.status === 'ready'
      ? state.data.servers.find((candidate) => candidate.id === serverId)
      : undefined
    if (server?.provisioning?.locked) return

    if (topology === 'standalone') {
      setRoles(next === 'none' ? {} : { [serverId]: 'control-plane' })
      setWorkloadControllers(next === 'none' ? {} : { [serverId]: true })
      return
    }

    setRoles((current) => {
      const updated = { ...current, [serverId]: next }
      if (topology === 'multi-node' && next === 'control-plane') {
        for (const [candidateID, role] of Object.entries(updated)) {
          if (candidateID !== serverId && role === 'control-plane') updated[candidateID] = 'none'
        }
      }
      return updated
    })
    if (next !== 'control-plane') {
      setWorkloadControllers((current) => ({ ...current, [serverId]: false }))
    }
  }

  const deploy = async () => {
    if (!effectiveSiteId || !basicsValid || !machinesValid || !networkingValid || submitting) return
    setSubmitting(true)
    try {
      const result = await clusters.deployCluster({
        siteId: effectiveSiteId,
        name: name.trim(),
        gpuStackOwner,
        k0sVersion: k0sVersion.trim(),
        ...(topology === 'high-availability'
          ? { apiVip: apiVip.trim(), apiVipPrefix: Number(apiVipPrefix) }
          : {}),
        podCidr: podCidr.trim(),
        serviceCidr: serviceCidr.trim(),
        roleAssignments: assignments,
      })
      showToast({
        title: 'Cluster deployment started',
        description: 'Lifecycle and membership will update on the Cluster page. Detailed automation output remains available when troubleshooting.',
        tone: 'success',
      })
      navigate(scopedHref(`/clusters/${result.clusterId}`))
    } catch (error) {
      showToast({
        title: 'Deployment failed',
        description: error instanceof Error ? error.message : 'Could not start the deployment.',
        tone: 'error',
      })
      setSubmitting(false)
    }
  }

  return (
    <div className="operator-page">
      <PageHeader
        title="Deploy cluster"
        breadcrumbs={[{ label: 'Clusters', href: scopedHref('/clusters') }, { label: 'Deploy' }]}
        subtitle="Build a standalone, non-HA multi-node, or highly available k0s cluster on deployed Servers."
      />
      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && (
        <Wizard
          aria-label="Deploy cluster"
          className="sw-deploy-wizard"
          height="min(700px, calc(100vh - 220px))"
          isVisitRequired
          shouldFocusContent
          onClose={() => navigate(scopedHref('/clusters'))}
          onSave={() => void deploy()}
        >
          <WizardStep
            name="Basics"
            id="deploy-basics"
            status={basicsValid ? 'success' : 'default'}
            footer={{ isNextDisabled: !basicsValid }}
          >
            <WizardSection title="Cluster identity">
              <Form className="sw-form-grid">
                <FormGroup label="Site" isRequired fieldId="cluster-site">
                  <SingleSelect
                    id="cluster-site"
                    ariaLabel="Site"
                    value={effectiveSiteId ?? ''}
                    placeholder="Select a Site"
                    options={state.data.sites.map((site) => ({
                      value: site.id,
                      label: site.name,
                    }))}
                    isRequired
                    onChange={(value) => {
                      setSiteId(value)
                      setRoles({})
                      setWorkloadControllers({})
                      setAPIVip('')
                    }}
                  />
                </FormGroup>
                <FormGroup label="Cluster name" isRequired fieldId="cluster-name">
                  <TextInput
                    id="cluster-name"
                    value={name}
                    onChange={(_event, value) => setName(value)}
                    placeholder="lab-k0s"
                  />
                </FormGroup>
                <FormGroup label="GPU stack owner" isRequired fieldId="cluster-gpu-owner">
                  <FormSelect
                    id="cluster-gpu-owner"
                    value={gpuStackOwner}
                    onChange={(_event, value) => setGPUStackOwner(value as GPUStackOwner)}
                  >
                    <FormSelectOption value="provisioning" label="Provisioning" />
                    <FormSelectOption value="gpu-operator" label="GPU Operator" />
                  </FormSelect>
                </FormGroup>
                <FormGroup label="k0s version" isRequired fieldId="cluster-version">
                  <TextInput
                    id="cluster-version"
                    value={k0sVersion}
                    onChange={(_event, value) => setK0sVersion(value)}
                  />
                </FormGroup>
              </Form>
            </WizardSection>
          </WizardStep>

          <WizardStep
            name="Machines"
            id="deploy-machines"
            status={machinesValid ? 'success' : 'default'}
            footer={{ isNextDisabled: !machinesValid }}
          >
            <WizardSection title="Topology and machines">
              <FormGroup label="Topology" isRequired fieldId="cluster-topology">
                <FormSelect
                  id="cluster-topology"
                  value={topology}
                  onChange={(_event, value) => changeTopology(value as TopologyChoice)}
                >
                  <FormSelectOption value="standalone" label="Standalone (single Server)" />
                  <FormSelectOption value="multi-node" label="Multi-node (non-HA)" />
                  <FormSelectOption value="high-availability" label="High availability" />
                </FormSelect>
              </FormGroup>
              <Alert
                variant={topology === 'high-availability' ? AlertVariant.info : AlertVariant.warning}
                title={topologyPresentation(topology).label}
                isInline
              >
                {topologyPresentation(topology).guidance}
              </Alert>
              <LabelGroup aria-label="Topology status">
                <Label color={controllers > 0 ? 'green' : 'orange'}>{controllers} control-plane</Label>
                <Label color={workloadCount > 0 ? 'green' : 'orange'}>{workloadCount} workload-capable</Label>
                <Label color={assignments.length > 0 ? 'blue' : 'grey'}>{assignments.length} selected</Label>
              </LabelGroup>
              {(hasLockedServers || hasAssignedServers) && (
                <Alert
                  variant={AlertVariant.warning}
                  title={hasLockedServers
                    ? hasAssignedServers
                      ? 'Some Servers are assigned or locked'
                      : 'Some Servers are locked'
                    : 'Some Servers are already assigned'}
                  isInline
                >
                  {hasLockedServers
                    ? hasAssignedServers
                      ? 'Assigned and Locked Servers remain visible for context. Unlock protected Servers or remove an existing Cluster assignment before selecting a role.'
                      : 'Locked Servers remain visible for context. Unlock protected Servers before selecting a role.'
                    : 'Assigned Servers remain visible for context. Remove the existing Cluster assignment before selecting a role.'}
                </Alert>
              )}
              {state.data.servers.length === 0 ? (
                <EmptyState
                  title="No deployed Servers"
                  message="Deploy an OS in this Site before building a Cluster."
                />
              ) : (
                <StickyTableFrame>
                  <Table aria-label="Deployable Servers" variant="compact">
                    <Thead>
                      <Tr><Th>Server</Th><Th>Address</Th><Th>Current assignment</Th><Th>Role</Th><Th>Runs workloads</Th></Tr>
                    </Thead>
                    <Tbody>
                      {state.data.servers.map((server) => {
                        const existing = existingClusterAssignment(
                          server,
                          state.data.clusters,
                          state.data.deploymentClaims,
                        )
                        const locked = server.provisioning?.locked ?? false
                        const unavailable = Boolean(existing) || locked
                        const role = unavailable ? 'none' : roles[server.id] ?? 'none'
                        return (
                          <Tr key={server.id}>
                            <Td dataLabel="Server"><span className="sw-machine-name"><strong>{serverDisplayName(server)}</strong><LockBadge locked={locked} /></span></Td>
                            <Td dataLabel="Address" className="sw-mono">
                              {serverPrimaryAddress(server) ?? '-'}
                            </Td>
                            <Td dataLabel="Current assignment">
                              {existing ? (
                                <LabelGroup>
                                  <Label color="red">In use</Label>
                                  <span>
                                    {existing.clusterName} ({existing.clusterType}, {existing.detail})
                                  </span>
                                </LabelGroup>
                              ) : '-'}
                            </Td>
                            <Td dataLabel="Role">
                              <FormSelect
                                aria-label={locked
                                  ? `Role for ${serverDisplayName(server)}, unavailable because the Server is locked`
                                  : existing
                                    ? `Role for ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.clusterName}`
                                  : `Role for ${serverDisplayName(server)}`}
                                value={role}
                                isDisabled={unavailable}
                                onChange={(_event, value) => changeRole(server.id, value as RoleChoice)}
                              >
                                <FormSelectOption value="none" label="Not included" />
                                <FormSelectOption
                                  value="control-plane"
                                  label={topology === 'standalone' ? 'Standalone node' : 'Control-plane'}
                                />
                                {topology !== 'standalone' && (
                                  <FormSelectOption value="worker" label="Worker" />
                                )}
                              </FormSelect>
                            </Td>
                            <Td dataLabel="Runs workloads">
                              {role === 'control-plane' ? (
                                <Checkbox
                                  id={`cluster-workload-${server.id}`}
                                  aria-label={`Run workloads on ${serverDisplayName(server)}`}
                                  isChecked={topology === 'standalone' || Boolean(workloadControllers[server.id])}
                                  isDisabled={topology === 'standalone'}
                                  onChange={(_event, checked) => setWorkloadControllers((current) => ({
                                    ...current,
                                    [server.id]: checked,
                                  }))}
                                />
                              ) : role === 'worker' ? <Label color="green">Yes</Label> : '-'}
                            </Td>
                          </Tr>
                        )
                      })}
                    </Tbody>
                  </Table>
                </StickyTableFrame>
              )}
            </WizardSection>
          </WizardStep>

          <WizardStep
            name="Networking"
            id="deploy-networking"
            status={networkingValid ? 'success' : 'default'}
            footer={{ isNextDisabled: !networkingValid }}
          >
            <WizardSection title="Cluster network">
              <section className="sw-network-context" aria-labelledby="selected-machine-addresses">
                <Title headingLevel="h3" size="md" id="selected-machine-addresses">
                  Selected machine addresses
                </Title>
                <LabelGroup aria-label="Selected machine addresses">
                  {selectedServers.map((server) => (
                    <Label key={server.id} color="grey">
                      {serverDisplayName(server)}: {serverPrimaryAddress(server) ?? '-'}
                    </Label>
                  ))}
                </LabelGroup>
                <p className="sw-muted">{selectedNetworkGuidance(selectedAddresses)}</p>
              </section>
              {topology === 'high-availability' ? (
                <Alert
                  variant={AlertVariant.info}
                  title="Allocate one unused virtual IP reachable by every selected machine"
                  isInline
                >
                  Swallow cannot verify DHCP reservations. Confirm the address is unused and outside dynamic ranges before deployment.
                </Alert>
              ) : (
                <Alert variant={AlertVariant.warning} title="Direct control-plane endpoint" isInline>
                  The Kubernetes API will use {initialControllerAddress ?? 'the selected control-plane address'}:6443. This topology has no endpoint failover.
                </Alert>
              )}
              <Form className="sw-form-grid">
                {topology === 'high-availability' && (
                  <>
                    <FormGroup label="API virtual IP" isRequired fieldId="cluster-api-vip">
                      <TextInput
                        id="cluster-api-vip"
                        value={apiVip}
                        onChange={(_event, value) => setAPIVip(value)}
                        placeholder="192.168.40.200"
                        validated={!apiVip || validAddress(apiVip) ? 'default' : 'error'}
                      />
                      <FormHelperText>
                        <HelperText>
                          <HelperTextItem variant={!apiVip || validAddress(apiVip) ? 'default' : 'error'}>
                            Enter an unused IPv4 address on the machine network.
                          </HelperTextItem>
                        </HelperText>
                      </FormHelperText>
                    </FormGroup>
                    <FormGroup label="VIP prefix length" isRequired fieldId="cluster-api-prefix">
                      <TextInput
                        id="cluster-api-prefix"
                        type="number"
                        value={apiVipPrefix}
                        onChange={(_event, value) => setAPIVipPrefix(value)}
                        min={1}
                        max={32}
                      />
                    </FormGroup>
                  </>
                )}
                <FormGroup label="Pod CIDR" isRequired fieldId="cluster-pod-cidr">
                  <TextInput
                    id="cluster-pod-cidr"
                    value={podCidr}
                    onChange={(_event, value) => setPodCidr(value)}
                    validated={!podCidr || validCIDR(podCidr) ? 'default' : 'error'}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem variant={!podCidr || validCIDR(podCidr) ? 'default' : 'error'}>
                        Default {DEFAULT_POD_CIDR}; must not overlap selected machine addresses.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
                <FormGroup label="Service CIDR" isRequired fieldId="cluster-service-cidr">
                  <TextInput
                    id="cluster-service-cidr"
                    value={serviceCidr}
                    onChange={(_event, value) => setServiceCidr(value)}
                    validated={!serviceCidr || validCIDR(serviceCidr) ? 'default' : 'error'}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem variant={!serviceCidr || validCIDR(serviceCidr) ? 'default' : 'error'}>
                        Default {DEFAULT_SERVICE_CIDR}; must not overlap selected machine addresses.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
              </Form>
            </WizardSection>
          </WizardStep>

          <WizardStep
            name="Review"
            id="deploy-review"
            status={basicsValid && machinesValid && networkingValid ? 'success' : 'warning'}
            footer={{
              nextButtonText: 'Deploy cluster',
              isNextDisabled: submitting || !basicsValid || !machinesValid || !networkingValid,
              nextButtonProps: { isLoading: submitting },
            }}
          >
            <WizardSection title="Review deployment">
              <Card isCompact>
                <CardTitle>{name}</CardTitle>
                <CardBody>
                  <DescriptionList isHorizontal isCompact>
                    {[
                      ['Site', state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId],
                      ['Topology', topologyPresentation(topology).label],
                      ['k0s version', k0sVersion],
                      ['GPU stack owner', gpuStackOwner],
                      ['API endpoint', topology === 'high-availability'
                        ? `${apiVip}/${apiVipPrefix} (virtual IP)`
                        : `${initialControllerAddress ?? '-'}:6443 (direct)`],
                      ['Pod CIDR', podCidr],
                      ['Service CIDR', serviceCidr],
                      ['Machines', `${assignments.length} selected`],
                      ['Roles', `${controllers} control-plane, ${workloadCount} workload-capable`],
                    ].map(([label, value]) => (
                      <DescriptionListGroup key={label}>
                        <DescriptionListTerm>{label}</DescriptionListTerm>
                        <DescriptionListDescription>{value}</DescriptionListDescription>
                      </DescriptionListGroup>
                    ))}
                  </DescriptionList>
                </CardBody>
              </Card>
              <Alert variant={AlertVariant.info} title="Submitting creates a Cluster and starts its deployment" isInline>
                You will continue on the Cluster page. Open detailed Operation output only when you need automation-level troubleshooting.
              </Alert>
            </WizardSection>
          </WizardStep>
        </Wizard>
      )}
    </div>
  )
}

/** Keeps every wizard step on the same compact vertical rhythm. */
function WizardSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="sw-wizard-section">
      <Title headingLevel="h2" size="lg">{title}</Title>
      {children}
    </section>
  )
}
