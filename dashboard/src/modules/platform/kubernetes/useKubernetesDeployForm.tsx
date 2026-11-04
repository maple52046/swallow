import { useMemo, useState } from 'react'
import { Badge, Field, Heading, HStack, Input, RadioCard, Switch, Text } from '@chakra-ui/react'
import { validIPv4Address, validIPv4CIDR } from '@/domain/network/ipv4'
import type { NodeRole, RoleAssignment } from '@/domain/platform/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import type { DeployStep, PlatformDeployContext, PlatformDeployForm } from '@/modules/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Select } from '@/presentation/components/ui/select'
import { AdvancedSettings } from '@/presentation/pages/platforms/AdvancedSettings'
import { PlatformGrafanaFeatureCard } from '@/presentation/pages/platforms/PlatformGrafanaFeatureCard'
import { EphemeralKubernetesWarning } from './EphemeralKubernetesWarning'

const DEFAULT_K0S_VERSION = 'v1.36.3+k0s.2'
const DEFAULT_POD_CIDR = '10.244.0.0/16'
const DEFAULT_SERVICE_CIDR = '10.96.0.0/12'
/** Where the deployment puts the Platform Grafana (platforms.md "Platform Monitoring Dashboards"). */
const PLATFORM_GRAFANA_PLACEMENT = 'in the cluster, at node port 30300 of every workload node'

type RoleChoice = 'none' | NodeRole
type TopologyChoice = 'standalone' | 'multi-node' | 'high-availability'

/**
 * The topology a new deploy (and a Site change) starts with: one control plane plus workers. It needs
 * the fewest decisions that still give a usable cluster; high availability asks for three or more
 * controllers and an unused virtual IP, which a user should choose deliberately.
 */
const DEFAULT_TOPOLOGY: TopologyChoice = 'multi-node'

/** Human labels and risk language for the three supported placement shapes. */
function topologyPresentation(topology: TopologyChoice) {
  switch (topology) {
    case 'standalone':
      return {
        label: 'Standalone',
        guidance: 'One Server runs the control plane and workloads. There is no control-plane failover, but the platform can be expanded later.',
      }
    case 'multi-node':
      return {
        label: 'Multi-node (non-HA)',
        guidance: 'One Server runs the control plane and one or more workers run workloads. A control-plane outage stops platform management.',
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
  const ipv4 = addresses.find(validIPv4Address)
  if (!ipv4) return 'Selected machines have no observed IPv4 address. Verify provider inventory before deployment.'
  const octets = ipv4.split('.')
  return `Observed addresses include ${octets.slice(0, 3).join('.')}.x. Confirm routing, DHCP ranges, and address reservations with the network owner.`
}

/**
 * The Kubernetes (k0s) deploy form: the k0s version, a topology with per-Server roles (starting,
 * and reset on a Site change, at `DEFAULT_TOPOLOGY`), the Platform Grafana feature, and the
 * Platform network step. Machine selection precedes networking
 * because the topology and the selected Servers' observed addresses decide whether a virtual IP
 * exists and give the user concrete allocation context.
 */
export function useKubernetesDeployForm({ servers, heldBy }: PlatformDeployContext): PlatformDeployForm {
  const [k0sVersion, setK0sVersion] = useState(DEFAULT_K0S_VERSION)
  const [topology, setTopology] = useState<TopologyChoice>(DEFAULT_TOPOLOGY)
  const [apiVip, setAPIVip] = useState('')
  // Empty means the backend takes the prefix of the control plane's subnet, which is the only
  // prefix it accepts when it can read that subnet.
  const [apiVipPrefix, setAPIVipPrefix] = useState('')
  const [podCidr, setPodCidr] = useState(DEFAULT_POD_CIDR)
  const [serviceCidr, setServiceCidr] = useState(DEFAULT_SERVICE_CIDR)
  const [roles, setRoles] = useState<Record<string, RoleChoice>>({})
  const [workloadControllers, setWorkloadControllers] = useState<Record<string, boolean>>({})
  // The spec's monitoringDashboards; on by default like the API's omitted value.
  const [platformGrafana, setPlatformGrafana] = useState(true)

  const assignments = useMemo<RoleAssignment[]>(
    () =>
      Object.entries(roles)
        .filter(([, role]) => role !== 'none')
        .map(([serverId, role]) => ({
          serverId,
          role: role as NodeRole,
          ...(role === 'control-plane' && workloadControllers[serverId] ? { runWorkloads: true } : {}),
        })),
    [roles, workloadControllers],
  )
  const workloadServerIds = useMemo(
    () => assignments.filter((assignment) => assignment.role === 'worker' || assignment.runWorkloads).map((assignment) => assignment.serverId),
    [assignments],
  )
  const controllers = assignments.filter((item) => item.role === 'control-plane').length
  const workers = assignments.filter((item) => item.role === 'worker').length
  const workloadCount = workloadServerIds.length
  const topologyValid =
    topology === 'standalone'
      ? assignments.length === 1 && controllers === 1 && workloadCount === 1
      : topology === 'multi-node'
        ? assignments.length >= 2 && controllers === 1 && workers >= 1
        : controllers >= 3 && controllers % 2 === 1 && workloadCount >= 1
  const networkingValid =
    validIPv4CIDR(podCidr.trim()) &&
    validIPv4CIDR(serviceCidr.trim()) &&
    (topology !== 'high-availability' || (validIPv4Address(apiVip.trim()) &&
      (apiVipPrefix.trim() === '' || (Number(apiVipPrefix) >= 1 && Number(apiVipPrefix) <= 32))))
  const initialControllerAddress = (selected: Server[]) => {
    const initialController = selected.find((server) => roles[server.id] === 'control-plane')
    return initialController ? serverPrimaryAddress(initialController) : null
  }

  const changeTopology = (next: TopologyChoice) => {
    setTopology(next)
    setRoles({})
    setWorkloadControllers({})
    setAPIVip('')
  }

  const changeRole = (serverId: string, next: RoleChoice) => {
    const server = servers.find((candidate) => candidate.id === serverId)
    if (server?.status.provisioning?.locked) return

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

  const roleCells = (server: Server, surface: 'table' | 'card') => {
    const held = heldBy(server)
    const role = held ? 'none' : roles[server.id] ?? 'none'
    const idPrefix = surface === 'card' ? 'mobile-' : ''
    return [
      <Select
        aria-label={held ? `Role for ${serverDisplayName(server)}, unavailable because it is ${held}` : `Role for ${serverDisplayName(server)}`}
        value={role}
        disabled={Boolean(held)}
        size="sm"
        onChange={(value) => changeRole(server.id, value as RoleChoice)}
        options={[
          { value: 'none', label: 'Not included' },
          { value: 'control-plane', label: topology === 'standalone' ? 'Standalone node' : 'Control-plane' },
          ...(topology !== 'standalone' ? [{ value: 'worker', label: 'Worker' }] : []),
        ]}
      />,
      role === 'control-plane' ? (
        <Switch.Root
          id={`${idPrefix}platform-workload-${server.id}`}
          checked={topology === 'standalone' || Boolean(workloadControllers[server.id])}
          disabled={topology === 'standalone'}
          colorPalette="brand"
          onCheckedChange={(details) => setWorkloadControllers((current) => ({ ...current, [server.id]: details.checked === true }))}
        >
          <Switch.HiddenInput aria-label={`Run workloads on ${serverDisplayName(server)}`} />
          <Switch.Control><Switch.Thumb /></Switch.Control>
        </Switch.Root>
      ) : role === 'worker' ? (
        <Badge colorPalette="green" variant="subtle">Yes</Badge>
      ) : (
        '-'
      ),
    ]
  }

  const networkingStep = (selected: Server[]): DeployStep => ({
    id: 'deploy-networking',
    name: 'Networking',
    title: 'Platform network',
    canProceed: networkingValid,
    blockedReason: 'Enter valid Kubernetes Pod and Service CIDRs, plus an API virtual IP for high availability.',
    content: (
      <>
        <section className="sw-network-context" aria-labelledby="selected-machine-addresses">
          <Heading as="h3" size="sm" id="selected-machine-addresses">
            Selected machine addresses
          </Heading>
          <HStack gap="2" wrap="wrap" aria-label="Selected machine addresses">
            {selected.map((server) => (
              <Badge key={server.id} colorPalette="gray" variant="subtle">
                {serverDisplayName(server)}: {serverPrimaryAddress(server) ?? '-'}
              </Badge>
            ))}
          </HStack>
          <Text className="sw-muted">{selectedNetworkGuidance(selected.flatMap((server) => server.status.addresses))}</Text>
        </section>
        {topology === 'high-availability' ? (
          <Alert status="info" title="Allocate one unused virtual IP reachable by every selected machine">
            Swallow cannot verify DHCP reservations. Confirm the address is unused and outside dynamic ranges before deployment.
          </Alert>
        ) : (
          <Alert status="warning" title="Direct control-plane endpoint">
            The Kubernetes API will use {initialControllerAddress(selected) ?? 'the selected control-plane address'}:6443. This topology has no endpoint failover.
          </Alert>
        )}
        {topology === 'high-availability' && (
          <Field.Root required invalid={Boolean(apiVip) && !validIPv4Address(apiVip)}>
            <Field.Label>API virtual IP</Field.Label>
            <Input value={apiVip} onChange={(event) => setAPIVip(event.target.value)} placeholder="192.168.40.200" />
            <Field.HelperText>Enter an unused IPv4 address on the machine network.</Field.HelperText>
            <Field.ErrorText>Enter an unused IPv4 address on the machine network.</Field.ErrorText>
          </Field.Root>
        )}
        <AdvancedSettings label="Advanced networking">
          <div className="sw-form-grid">
            {topology === 'high-availability' && (
              <Field.Root>
                <Field.Label>VIP prefix length</Field.Label>
                <Input
                  type="number"
                  value={apiVipPrefix}
                  onChange={(event) => setAPIVipPrefix(event.target.value)}
                  min={1}
                  max={32}
                  placeholder="Machine subnet prefix"
                />
                <Field.HelperText>Leave empty to use the machine subnet's prefix. A different prefix is refused.</Field.HelperText>
              </Field.Root>
            )}
            <Field.Root required invalid={Boolean(podCidr) && !validIPv4CIDR(podCidr)}>
              <Field.Label>Pod CIDR</Field.Label>
              <Input value={podCidr} onChange={(event) => setPodCidr(event.target.value)} />
              <Field.HelperText>Default {DEFAULT_POD_CIDR}; must not overlap selected machine addresses.</Field.HelperText>
              <Field.ErrorText>Default {DEFAULT_POD_CIDR}; must not overlap selected machine addresses.</Field.ErrorText>
            </Field.Root>
            <Field.Root required invalid={Boolean(serviceCidr) && !validIPv4CIDR(serviceCidr)}>
              <Field.Label>Service CIDR</Field.Label>
              <Input value={serviceCidr} onChange={(event) => setServiceCidr(event.target.value)} />
              <Field.HelperText>Default {DEFAULT_SERVICE_CIDR}; must not overlap selected machine addresses.</Field.HelperText>
              <Field.ErrorText>Default {DEFAULT_SERVICE_CIDR}; must not overlap selected machine addresses.</Field.ErrorText>
            </Field.Root>
          </div>
        </AdvancedSettings>
      </>
    ),
  })

  return {
    basicsFields: (
      <Field.Root required>
        <Field.Label>k0s version</Field.Label>
        <Input value={k0sVersion} onChange={(event) => setK0sVersion(event.target.value)} />
      </Field.Root>
    ),
    basicsExtras: null,
    basicsValid: Boolean(k0sVersion.trim()),
    basicsBlockedReason: 'Enter a k0s version.',
    machines: {
      name: 'Topology & Servers',
      title: 'Topology and machines',
      intro: (
        <>
          <Field.Root required>
            <Field.Label>Topology</Field.Label>
            <RadioCard.Root
              aria-label="Topology"
              colorPalette="brand"
              value={topology}
              className="sw-platform-topology-grid"
              onValueChange={(details) => changeTopology(details.value as TopologyChoice)}
            >
              {(['standalone', 'multi-node', 'high-availability'] as const).map((choice) => {
                const presentation = topologyPresentation(choice)
                return (
                  <RadioCard.Item key={choice} value={choice}>
                    <RadioCard.ItemHiddenInput />
                    <RadioCard.ItemControl className="sw-platform-option-card">
                      <RadioCard.ItemContent>
                        <RadioCard.ItemText>{presentation.label}</RadioCard.ItemText>
                        <RadioCard.ItemDescription>{presentation.guidance}</RadioCard.ItemDescription>
                      </RadioCard.ItemContent>
                      <RadioCard.ItemIndicator />
                    </RadioCard.ItemControl>
                  </RadioCard.Item>
                )
              })}
            </RadioCard.Root>
          </Field.Root>
          <HStack gap="2" wrap="wrap">
            <Badge colorPalette={controllers > 0 ? 'green' : 'orange'} variant="subtle">{controllers} control-plane</Badge>
            <Badge colorPalette={workloadCount > 0 ? 'green' : 'orange'} variant="subtle">{workloadCount} workload-capable</Badge>
            <Badge colorPalette={assignments.length > 0 ? 'blue' : 'gray'} variant="subtle">{assignments.length} selected</Badge>
          </HStack>
        </>
      ),
      roleHeaders: ['Role', 'Runs workloads'],
      roleCells,
      valid: topologyValid,
      blockedReason: 'Complete the selected topology with the required control-plane and workload Servers.',
    },
    selectedServerIds: assignments.map((assignment) => assignment.serverId),
    featureContext: { servers, workloadServerIds, platformVersion: k0sVersion },
    features: {
      title: 'Kubernetes features',
      content: (
        <div className="sw-platform-feature-list">
          <PlatformGrafanaFeatureCard
            placement={PLATFORM_GRAFANA_PLACEMENT}
            dashboards="a Servers dashboard, plus GPU dashboards when the AMD GPU Operator runs its Device Metrics Exporter"
            checked={platformGrafana}
            onCheckedChange={setPlatformGrafana}
          />
        </div>
      ),
      valid: true,
    },
    extraSteps: (selected) => [networkingStep(selected)],
    reviewGroups: ({ name, site, preparationSummary }) => [
      {
        title: 'Platform',
        stepId: 'deploy-basics',
        items: [
          { label: 'Name', value: name },
          { label: 'Site', value: site },
          { label: 'k0s version', value: k0sVersion },
        ],
      },
      {
        title: 'Topology and Servers',
        stepId: 'deploy-machines',
        items: [
          { label: 'Topology', value: topologyPresentation(topology).label },
          { label: 'Servers', value: `${assignments.length} selected` },
          { label: 'Roles', value: `${controllers} control-plane, ${workloadCount} workload-capable` },
          { label: 'Operating system', value: preparationSummary },
        ],
      },
      {
        title: 'Monitoring',
        stepId: 'deploy-features',
        items: [
          { label: 'Platform Grafana', value: platformGrafana ? `In the cluster, at port 30300 of every workload node` : 'Not included' },
        ],
      },
    ],
    reviewGroupsAfterOS: (selected) => [
      {
        title: 'Networking',
        stepId: 'deploy-networking',
        items: [
          {
            label: 'API endpoint',
            value: topology === 'high-availability'
              ? (apiVipPrefix.trim()
                  ? `${apiVip}/${apiVipPrefix} (virtual IP)`
                  : `${apiVip} (virtual IP, machine subnet prefix)`)
              : `${initialControllerAddress(selected) ?? '-'}:6443 (direct)`,
          },
          { label: 'Pod CIDR', value: podCidr },
          { label: 'Service CIDR', value: serviceCidr },
        ],
      },
    ],
    deployModeNotice: (ephemeral) => (ephemeral ? <EphemeralKubernetesWarning /> : null),
    input: () => ({
      k0sVersion: k0sVersion.trim(),
      ...(topology === 'high-availability'
        ? { apiVip: apiVip.trim(), ...(apiVipPrefix.trim() ? { apiVipPrefix: Number(apiVipPrefix) } : {}) }
        : {}),
      podCidr: podCidr.trim(),
      serviceCidr: serviceCidr.trim(),
      monitoringDashboards: platformGrafana,
      roleAssignments: assignments,
    }),
    reset: () => {
      setTopology(DEFAULT_TOPOLOGY)
      setRoles({})
      setWorkloadControllers({})
      setAPIVip('')
      setAPIVipPrefix('')
      setPlatformGrafana(true)
    },
  }
}
