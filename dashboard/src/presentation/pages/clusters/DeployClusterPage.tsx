import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Box,
  Button,
  Callout,
  Card,
  Flex,
  Grid,
  Heading,
  Select,
  Table,
  Text,
  TextField,
} from '@radix-ui/themes'
import { InfoCircledIcon, RocketIcon } from '@radix-ui/react-icons'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/radix/toast/toastContext'
import { serverDisplayName, serverPrimaryAddress } from '@/domain/server/types'
import type { GPUStackOwner, NodeRole, RoleAssignment } from '@/domain/cluster/types'
import { useDeployableServers } from './useDeployableServers'

/** The k0s version the HA knowledge base last verified on this lab. */
const DEFAULT_K0S_VERSION = 'v1.36.3+k0s.2'
const DEFAULT_VIP_PREFIX = 24

/** A per-server choice in the wizard: excluded, or included as a control-plane / worker. */
type RoleChoice = 'none' | NodeRole

/**
 * The cluster deployment wizard: pick a site, assign server roles, set k0s parameters, and
 * start the deployment.
 *
 * Role assignment is the load-bearing input: swallow needs an odd number of at least three
 * control-plane servers for an etcd quorum, so the form counts them live and only enables
 * submit when the shape is valid. The backend re-validates everything; the client checks
 * only enough to avoid an obviously rejected request. On success it navigates to the
 * operation building the cluster, which is where progress and logs are.
 */
export function DeployClusterPage() {
  const navigate = useNavigate()
  const { clusters } = useApp()
  const { showToast } = useToast()

  const [siteId, setSiteId] = useState<string | undefined>(undefined)
  const state = useDeployableServers(siteId)

  const [name, setName] = useState('')
  const [gpuStackOwner, setGpuStackOwner] = useState<GPUStackOwner>('provisioning')
  const [k0sVersion, setK0sVersion] = useState(DEFAULT_K0S_VERSION)
  const [apiVip, setApiVip] = useState('')
  const [apiVipPrefix, setApiVipPrefix] = useState(String(DEFAULT_VIP_PREFIX))
  const [podCidr, setPodCidr] = useState('')
  const [serviceCidr, setServiceCidr] = useState('')
  const [roles, setRoles] = useState<Record<string, RoleChoice>>({})
  const [submitting, setSubmitting] = useState(false)

  // Default the site to the only one when there is exactly one, so the common case skips a
  // choice; reset role choices whenever the site changes so they never point at absent servers.
  useEffect(() => {
    if (state.status === 'ready' && siteId === undefined && state.data.sites.length === 1) {
      setSiteId(state.data.sites[0].id)
    }
  }, [state, siteId])
  useEffect(() => {
    setRoles({})
  }, [siteId])

  const assignments = useMemo<RoleAssignment[]>(() => {
    return Object.entries(roles)
      .filter(([, choice]) => choice !== 'none')
      .map(([serverId, choice]) => ({ serverId, role: choice as NodeRole }))
  }, [roles])

  const controllerCount = assignments.filter((a) => a.role === 'control-plane').length
  const workerCount = assignments.filter((a) => a.role === 'worker').length

  const controllersValid = controllerCount >= 3 && controllerCount % 2 === 1
  const canSubmit =
    siteId !== undefined &&
    name.trim().length > 0 &&
    apiVip.trim().length > 0 &&
    controllersValid &&
    workerCount >= 1 &&
    !submitting

  const onSubmit = async () => {
    if (!siteId || !canSubmit) return
    setSubmitting(true)
    try {
      const result = await clusters.deployCluster({
        siteId,
        name: name.trim(),
        gpuStackOwner,
        k0sVersion: k0sVersion.trim(),
        apiVip: apiVip.trim(),
        apiVipPrefix: Number(apiVipPrefix) || DEFAULT_VIP_PREFIX,
        podCidr: podCidr.trim() || undefined,
        serviceCidr: serviceCidr.trim() || undefined,
        roleAssignments: assignments,
      })
      showToast({
        tone: 'success',
        title: 'Deployment started',
        description: 'Building the cluster; follow its progress on the operation.',
      })
      navigate(`/operations/${result.operationId}`)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Could not start the deployment.'
      showToast({ tone: 'error', title: 'Deployment failed', description: message })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Box>
      <PageHeader
        title="Deploy cluster"
        subtitle="Build a highly available k0s cluster on your deployed servers."
      />

      {state.status === 'loading' && <LoadingState rows={6} />}
      {state.status === 'error' && <ErrorState message={state.message} />}

      {state.status === 'ready' && (
        <Flex direction="column" gap="4">
          <Card>
            <Heading size="3" mb="3">
              Cluster
            </Heading>
            <Grid columns={{ initial: '1', md: '2' }} gap="3">
              <Field label="Site">
                <Select.Root
                  value={siteId ?? ''}
                  onValueChange={(value) => setSiteId(value)}
                >
                  <Select.Trigger placeholder="Select a site" aria-label="Site" />
                  <Select.Content>
                    {state.data.sites.map((site) => (
                      <Select.Item key={site.id} value={site.id}>
                        {site.name}
                      </Select.Item>
                    ))}
                  </Select.Content>
                </Select.Root>
              </Field>
              <Field label="Name">
                <TextField.Root
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="lab-k0s"
                  aria-label="Cluster name"
                />
              </Field>
              <Field label="GPU stack owner">
                <Select.Root
                  value={gpuStackOwner}
                  onValueChange={(value) => setGpuStackOwner(value as GPUStackOwner)}
                >
                  <Select.Trigger aria-label="GPU stack owner" />
                  <Select.Content>
                    <Select.Item value="provisioning">provisioning</Select.Item>
                    <Select.Item value="gpu-operator">gpu-operator</Select.Item>
                  </Select.Content>
                </Select.Root>
              </Field>
              <Field label="k0s version">
                <TextField.Root
                  value={k0sVersion}
                  onChange={(event) => setK0sVersion(event.target.value)}
                  aria-label="k0s version"
                />
              </Field>
            </Grid>
          </Card>

          <Card>
            <Heading size="3" mb="1">
              Networking
            </Heading>
            <Text size="1" color="gray" mb="3" as="p">
              The API virtual IP floats across the controllers. It must be routable on the
              node network and outside any DHCP range. Pod and service ranges are optional and
              must not cover the nodes' own subnet; leave them blank for the defaults.
            </Text>
            <Grid columns={{ initial: '1', md: '2' }} gap="3">
              <Field label="API virtual IP">
                <TextField.Root
                  value={apiVip}
                  onChange={(event) => setApiVip(event.target.value)}
                  placeholder="192.168.100.200"
                  aria-label="API virtual IP"
                />
              </Field>
              <Field label="VIP prefix length">
                <TextField.Root
                  value={apiVipPrefix}
                  onChange={(event) => setApiVipPrefix(event.target.value)}
                  placeholder="24"
                  aria-label="VIP prefix length"
                />
              </Field>
              <Field label="Pod CIDR (optional)">
                <TextField.Root
                  value={podCidr}
                  onChange={(event) => setPodCidr(event.target.value)}
                  placeholder="10.244.0.0/16"
                  aria-label="Pod CIDR"
                />
              </Field>
              <Field label="Service CIDR (optional)">
                <TextField.Root
                  value={serviceCidr}
                  onChange={(event) => setServiceCidr(event.target.value)}
                  placeholder="10.96.0.0/12"
                  aria-label="Service CIDR"
                />
              </Field>
            </Grid>
          </Card>

          <Card>
            <Flex justify="between" align="center" mb="2" wrap="wrap" gap="2">
              <Heading size="3">Roles</Heading>
              <Flex gap="2" align="center">
                <Text size="2" color={controllersValid ? 'green' : 'gray'}>
                  {controllerCount} control-plane
                </Text>
                <Text size="2" color={workerCount >= 1 ? 'green' : 'gray'}>
                  {workerCount} workers
                </Text>
              </Flex>
            </Flex>

            <Callout.Root color="blue" mb="3" size="1">
              <Callout.Icon>
                <InfoCircledIcon />
              </Callout.Icon>
              <Callout.Text>
                A highly available control plane needs an odd number of at least three
                control-plane servers, plus at least one worker.
              </Callout.Text>
            </Callout.Root>

            {siteId === undefined ? (
              <EmptyState title="Select a site" message="Choose a site to list its deployed servers." />
            ) : state.data.servers.length === 0 ? (
              <EmptyState
                title="No deployed servers"
                message="This site has no deployed servers to build a cluster from. Deploy an OS to some servers first."
              />
            ) : (
              <Table.Root variant="surface">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeaderCell>Server</Table.ColumnHeaderCell>
                    <Table.ColumnHeaderCell>Address</Table.ColumnHeaderCell>
                    <Table.ColumnHeaderCell>Role</Table.ColumnHeaderCell>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {state.data.servers.map((server) => (
                    <Table.Row key={server.id}>
                      <Table.Cell>
                        <Text size="2" weight="medium">
                          {serverDisplayName(server)}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="1" style={{ fontFamily: 'monospace' }}>
                          {serverPrimaryAddress(server) ?? '—'}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Select.Root
                          value={roles[server.id] ?? 'none'}
                          onValueChange={(value) =>
                            setRoles((previous) => ({
                              ...previous,
                              [server.id]: value as RoleChoice,
                            }))
                          }
                        >
                          <Select.Trigger aria-label={`Role for ${serverDisplayName(server)}`} />
                          <Select.Content>
                            <Select.Item value="none">Not in cluster</Select.Item>
                            <Select.Item value="control-plane">control-plane</Select.Item>
                            <Select.Item value="worker">worker</Select.Item>
                          </Select.Content>
                        </Select.Root>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            )}
          </Card>

          <Flex justify="end" gap="2">
            <Button variant="soft" color="gray" onClick={() => navigate('/clusters')}>
              Cancel
            </Button>
            <Button onClick={onSubmit} loading={submitting} disabled={!canSubmit}>
              <RocketIcon />
              Deploy cluster
            </Button>
          </Flex>
        </Flex>
      )}
    </Box>
  )
}

/** A labelled form field; keeps label/control spacing consistent across the wizard. */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Flex direction="column" gap="1">
      <Text size="2" weight="medium">
        {label}
      </Text>
      {children}
    </Flex>
  )
}
