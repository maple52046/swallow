import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Divider,
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
import type { ProvisionerDetail, Server, ServerAction } from '@/domain/server/types'
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

  // Provisioner-action feedback is kept separate from the deploy/release feedback so it
  // can render next to the action buttons that produced it, rather than in the OS card.
  const [actionsError, setActionsError] = useState<string | null>(null)
  const [actionsNote, setActionsNote] = useState<string | null>(null)

  const [detail, setDetail] = useState<ProvisionerDetail | null>(null)
  const [detailError, setDetailError] = useState<string | null>(null)

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

  // The provisioner detail is read live, one machine at a time, so it is fetched
  // separately from the mirrored projection and refetched with `detailNonce` after an
  // action changes the machine.
  const [detailNonce, setDetailNonce] = useState(0)
  useEffect(() => {
    if (!server) return
    let cancelled = false

    servers
      .getProvisionerDetail(server.id)
      .then((result) => {
        if (cancelled) return
        setDetail(result)
        setDetailError(null)
      })
      .catch((err: Error) => {
        if (cancelled) return
        setDetail(null)
        setDetailError(err.message)
      })

    return () => {
      cancelled = true
    }
  }, [servers, server, detailNonce])

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

  const runProvisionerAction = async (action: ServerAction) => {
    if (!server) return
    setBusy(true)
    setActionsError(null)
    setActionsNote(null)

    try {
      const result = await servers.runServerAction(server.id, action)
      setActionsNote(
        `Accepted "${action}". The provisioner reports "${result.state}"; the reconciler will follow it from here.`,
      )
      load()
      // Re-read the live detail, since the action changed the machine.
      setDetailNonce((n) => n + 1)
    } catch (err) {
      setActionsError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const queryPower = async () => {
    if (!server) return
    setActionsError(null)
    setActionsNote(null)
    try {
      const result = await servers.queryPowerState(server.id)
      setActionsNote(`Live power state: ${result.powerState}.`)
    } catch (err) {
      setActionsError((err as Error).message)
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
                <Field label="Commissioning" value={server.provisioning.commissioningStatus || null} />
                <Field label="Testing" value={server.provisioning.testingStatus || null} />
                {server.provisioning.locked && <Field label="Locked" value="yes" />}
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
              <Field label="CPU model" value={server.cpuModel || null} />
              <Field
                label="Memory"
                value={server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : null}
              />
              <Field
                label="Storage"
                value={server.storageGB ? `${Math.round(server.storageGB)} GB` : null}
              />
              <Field label="System vendor" value={server.systemVendor || null} />
              <Field label="System product" value={server.systemProduct || null} />
              <Field label="Provisioner zone" value={server.providerZone || null} />
              <Field label="Resource pool" value={server.providerResourcePool || null} />
              <Field label="VM host" value={server.providerPod || null} />
              <Field
                label="Tags"
                value={server.tags.length > 0 ? server.tags.join(', ') : null}
              />
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

          <Card withBorder mt="md">
            <Title order={5} mb="sm">
              Provisioner detail
            </Title>
            <Text size="xs" c="dimmed" mb="sm">
              Read live from the provisioner for this machine. Not stored by gdcm, so it
              is always current and reflects the provisioner exactly.
            </Text>

            {detailError && (
              <Alert icon={<IconAlertCircle size={16} />} color="gray">
                Could not read live detail from the provisioner: {detailError}
              </Alert>
            )}

            {!detailError && !detail && <Text size="sm" c="dimmed">Loading…</Text>}

            {!detailError && detail && detail.sections.length === 0 && detail.tables.length === 0 && (
              <Text size="sm" c="dimmed">
                This provisioner offers no additional detail.
              </Text>
            )}

            {detail?.sections.map((section) => (
              <div key={section.title} style={{ marginBottom: 'var(--mantine-spacing-md)' }}>
                <Text size="sm" fw={600} mb={4}>
                  {section.title}
                </Text>
                <SimpleGrid cols={2} spacing="sm">
                  {section.fields.map((field) => (
                    <Field key={field.label} label={field.label} value={field.value || null} />
                  ))}
                </SimpleGrid>
              </div>
            ))}

            {detail?.tables.map((table) => (
              <div key={table.title} style={{ marginBottom: 'var(--mantine-spacing-md)' }}>
                <Text size="sm" fw={600} mb={4}>
                  {table.title}
                </Text>
                <Table>
                  <Table.Thead>
                    <Table.Tr>
                      {table.columns.map((column) => (
                        <Table.Th key={column}>{column}</Table.Th>
                      ))}
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {table.rows.map((row, rowIndex) => (
                      <Table.Tr key={`${table.title}-${rowIndex}`}>
                        {row.map((cell, cellIndex) => (
                          <Table.Td key={`${table.title}-${rowIndex}-${cellIndex}`}>{cell}</Table.Td>
                        ))}
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </div>
            ))}
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

          {detail?.capabilities &&
            (detail.capabilities.power ||
              detail.capabilities.hardwareValidation ||
              detail.capabilities.operatorState) && (
              <Card withBorder mt="md">
                <Title order={5} mb="sm">
                  Provisioner actions
                </Title>
                <Text size="xs" c="dimmed" mb="sm">
                  Only the actions this provisioner supports are shown. Each is carried out
                  by the provisioner; gdcm mirrors the result.
                </Text>

                {actionsError && (
                  <Alert icon={<IconAlertCircle size={16} />} color="red" mb="sm">
                    {actionsError}
                  </Alert>
                )}
                {actionsNote && (
                  <Alert color="blue" mb="sm">
                    {actionsNote}
                  </Alert>
                )}

                <Stack gap="sm">
                  {detail.capabilities.power && (
                    <div>
                      <Text size="xs" c="dimmed" mb={4}>
                        Power
                      </Text>
                      <Group gap="xs">
                        <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('power-on')}>
                          Power on
                        </Button>
                        <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('power-off')}>
                          Power off
                        </Button>
                        <Button size="xs" variant="subtle" onClick={queryPower}>
                          Query state
                        </Button>
                      </Group>
                    </div>
                  )}

                  {detail.capabilities.hardwareValidation && (
                    <>
                      <Divider />
                      <div>
                        <Text size="xs" c="dimmed" mb={4}>
                          Hardware validation
                        </Text>
                        <Group gap="xs">
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('commission')}>
                            Commission
                          </Button>
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('test')}>
                            Test
                          </Button>
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('abort')}>
                            Abort
                          </Button>
                          <Button size="xs" variant="subtle" loading={busy} onClick={() => runProvisionerAction('override-failed-testing')}>
                            Override failed testing
                          </Button>
                        </Group>
                      </div>
                    </>
                  )}

                  {detail.capabilities.operatorState && (
                    <>
                      <Divider />
                      <div>
                        <Text size="xs" c="dimmed" mb={4}>
                          Operator state
                        </Text>
                        <Group gap="xs">
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('lock')}>
                            Lock
                          </Button>
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('unlock')}>
                            Unlock
                          </Button>
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('mark-broken')}>
                            Mark broken
                          </Button>
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('mark-fixed')}>
                            Mark fixed
                          </Button>
                          <Button size="xs" variant="default" loading={busy} onClick={() => runProvisionerAction('rescue-mode')}>
                            Rescue mode
                          </Button>
                          <Button size="xs" variant="subtle" loading={busy} onClick={() => runProvisionerAction('exit-rescue-mode')}>
                            Exit rescue
                          </Button>
                        </Group>
                      </div>
                    </>
                  )}
                </Stack>
              </Card>
            )}
        </Grid.Col>
      </Grid>
    </>
  )
}
