import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Grid,
  Group,
  Select,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Textarea,
  Title,
} from '@mantine/core'
import { IconAlertCircle } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import {
  HealthBadge,
  MembershipBadge,
  ProvisioningBadge,
} from '@/presentation/components/AxisBadge'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import type { OSImage } from '@/domain/site/types'

function Field({ label, value }: { label: string; value: string | null }) {
  return (
    <div>
      <Text size="xs" c="dimmed">
        {label}
      </Text>
      <Text size="sm" c={value ? undefined : 'dimmed'}>
        {value ?? 'not observed'}
      </Text>
    </div>
  )
}

export function ServerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { servers, sites } = useApp()

  const [server, setServer] = useState<Server | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [images, setImages] = useState<OSImage[]>([])
  const [distroSeries, setDistroSeries] = useState<string | null>(null)
  const [userData, setUserData] = useState('')
  const [ephemeral, setEphemeral] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionNote, setActionNote] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    if (!id) return
    setLoading(true)
    setError(null)

    servers
      .getServer(id)
      .then(setServer)
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false))
  }, [servers, id])

  useEffect(load, [load])

  // Deployable images come from this server's own provisioner: they differ per site,
  // so a merged list would offer images this site cannot deploy.
  useEffect(() => {
    if (!server) return
    let cancelled = false

    sites
      .listOSImages(server.source.integrationId)
      .then((result) => {
        if (!cancelled) setImages(result)
      })
      .catch(() => {
        if (!cancelled) setImages([])
      })

    return () => {
      cancelled = true
    }
  }, [sites, server])

  const runAction = async (action: 'deploy' | 'release') => {
    if (!server) return
    setBusy(true)
    setActionError(null)
    setActionNote(null)

    try {
      const result =
        action === 'deploy'
          ? await servers.deployServer(server.id, {
              distroSeries: distroSeries ?? '',
              userData: userData || undefined,
              ephemeral: ephemeral || undefined,
            })
          : await servers.releaseServer(server.id)

      // The response is a snapshot from when the provisioner accepted the request, not
      // a completion. Saying so avoids implying the work is finished.
      setActionNote(
        `Accepted. The provisioner reports "${result.state}"; the reconciler will follow it from here.`,
      )
      load()
    } catch (err) {
      setActionError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} />
  if (!server) return <ErrorState message="Server not found." />

  const canDeploy = server.provisioning?.state === 'ready'
  const canRelease = server.provisioning?.state === 'deployed'

  return (
    <>
      <PageHeader
        title={serverDisplayName(server)}
        subtitle={`${server.source.providerMachineId} · site ${server.source.siteId}`}
        actions={
          server.absent ? (
            <Badge color="gray" variant="outline">
              absent from provisioner
            </Badge>
          ) : undefined
        }
      />

      {server.absent && (
        <Alert icon={<IconAlertCircle size={16} />} color="gray" mb="md" title="Not currently reported">
          The provisioner has stopped listing this machine. It is kept rather than deleted,
          because absence is usually transient.
        </Alert>
      )}

      <Grid>
        <Grid.Col span={{ base: 12, md: 7 }}>
          <Card withBorder mb="md">
            <Title order={5} mb="sm">
              Status
            </Title>
            <Text size="xs" c="dimmed" mb="sm">
              Three independent axes, each owned by a different system. An axis with no
              observation is unknown, which is not the same as bad.
            </Text>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed" mb={4}>
                  Provisioning
                </Text>
                <ProvisioningBadge axis={server.provisioning} />
              </div>
              <div>
                <Text size="xs" c="dimmed" mb={4}>
                  Cluster membership
                </Text>
                <MembershipBadge axis={server.membership} />
              </div>
              <div>
                <Text size="xs" c="dimmed" mb={4}>
                  Health
                </Text>
                <HealthBadge axis={server.health} />
              </div>
            </SimpleGrid>

            {server.provisioning && (
              <SimpleGrid cols={2} spacing="sm" mt="md">
                <Field
                  label="Deployed OS"
                  value={
                    server.provisioning.distroSeries
                      ? [server.provisioning.osSystem, server.provisioning.distroSeries]
                          .filter(Boolean)
                          .join(' ')
                      : null
                  }
                />
                <Field label="Kernel" value={server.provisioning.hweKernel || null} />
              </SimpleGrid>
            )}
          </Card>

          <Card withBorder mb="md">
            <Title order={5} mb="sm">
              Observed
            </Title>
            <SimpleGrid cols={2} spacing="sm">
              <Field label="Hostname" value={server.hostname} />
              <Field label="FQDN" value={server.fqdn} />
              <Field
                label="Addresses"
                value={server.addresses.length > 0 ? server.addresses.join(', ') : null}
              />
              <Field label="Architecture" value={server.architecture || null} />
              <Field label="CPU cores" value={server.cpuCores ? String(server.cpuCores) : null} />
              <Field
                label="Memory"
                value={server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : null}
              />
              <Field
                label="Storage"
                value={server.storageGB ? `${Math.round(server.storageGB)} GB` : null}
              />
              <Field label="Provisioner zone" value={server.providerZone || null} />
              <Field label="Resource pool" value={server.providerResourcePool || null} />
            </SimpleGrid>
          </Card>

          {server.gpus.length > 0 && (
            <Card withBorder mb="md">
              <Title order={5} mb="sm">
                GPUs
              </Title>
              <Table>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Vendor</Table.Th>
                    <Table.Th>Model</Table.Th>
                    <Table.Th>Count</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {server.gpus.map((gpu, index) => (
                    <Table.Tr key={`${gpu.vendor}-${gpu.model}-${index}`}>
                      <Table.Td>{gpu.vendor}</Table.Td>
                      <Table.Td>{gpu.model}</Table.Td>
                      <Table.Td>{gpu.count}</Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Card>
          )}

          <Card withBorder>
            <Title order={5} mb="sm">
              Identity
            </Title>
            <SimpleGrid cols={2} spacing="sm">
              <Field label="Server ID" value={server.id} />
              <Field label="Provisioner machine ID" value={server.source.providerMachineId} />
              <Field label="System UUID" value={server.hardware.systemUuid} />
              <Field label="Serial number" value={server.hardware.serialNumber} />
              <Field
                label="MAC addresses"
                value={
                  server.hardware.macAddresses.length > 0
                    ? server.hardware.macAddresses.join(', ')
                    : null
                }
              />
              <Field label="Last seen" value={server.lastSeenAt} />
            </SimpleGrid>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 5 }}>
          <Card withBorder>
            <Title order={5} mb="sm">
              Operating system
            </Title>

            {actionError && (
              <Alert icon={<IconAlertCircle size={16} />} color="red" mb="sm">
                {actionError}
              </Alert>
            )}
            {actionNote && (
              <Alert color="blue" mb="sm">
                {actionNote}
              </Alert>
            )}

            <Stack gap="sm">
              <Select
                label="Image"
                description={
                  images.length === 0
                    ? 'This provisioner reported no deployable images.'
                    : 'Required: the provisioner must not choose for you.'
                }
                placeholder="Select an image"
                data={images.map((image) => ({ value: image.id, label: image.name }))}
                value={distroSeries}
                onChange={setDistroSeries}
                disabled={!canDeploy || images.length === 0}
                searchable
              />

              <Textarea
                label="Cloud-init user data"
                description="Optional. Plain text; the backend encodes it."
                placeholder="#cloud-config"
                value={userData}
                onChange={(event) => setUserData(event.currentTarget.value)}
                disabled={!canDeploy}
                autosize
                minRows={3}
                maxRows={8}
              />

              <Checkbox
                label="Deploy in memory"
                description="The disks are left untouched and the OS runs from RAM, so anything written to the root filesystem is lost on reboot. A provisioner that cannot do this refuses the deployment rather than installing to disk."
                checked={ephemeral}
                onChange={(event) => setEphemeral(event.currentTarget.checked)}
                disabled={!canDeploy}
              />

              {ephemeral && (
                <Alert color="orange" icon={<IconAlertCircle size={16} />}>
                  Nothing installed afterwards will survive a reboot, including anything
                  an operation configures on this machine.
                </Alert>
              )}

              <Group>
                <Button
                  onClick={() => runAction('deploy')}
                  loading={busy}
                  disabled={!canDeploy || !distroSeries}
                >
                  Deploy
                </Button>
                <Button
                  variant="default"
                  onClick={() => runAction('release')}
                  loading={busy}
                  disabled={!canRelease}
                >
                  Release
                </Button>
              </Group>

              {!canDeploy && !canRelease && (
                <Text size="xs" c="dimmed">
                  Deploying needs the machine to be ready, and releasing needs it to be
                  deployed. It is currently {server.provisioning?.state ?? 'unknown'}.
                </Text>
              )}
              {canRelease && (
                <Text size="xs" c="dimmed">
                  Releasing returns the machine to the provisioner's pool so it can be
                  deployed again. It does not remove the server.
                </Text>
              )}
            </Stack>
          </Card>
        </Grid.Col>
      </Grid>
    </>
  )
}
