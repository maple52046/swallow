import { useCallback, useState } from 'react'
import { Badge, Button, Field, HStack, Input, SimpleGrid, Stack, Text } from '@chakra-ui/react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { DOCKER_OBJECT_NAME_PATTERN, shortDockerId, type DockerNetwork } from '@/domain/software/docker'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { ResponsiveResourceList } from '@/presentation/components/ResponsiveResourceList'
import { DOCKER_EXPLORER_UNAVAILABLE_HINT, type DockerSectionProps } from './dockerPresentation'
import { useDockerMutation } from './useDockerMutation'

// The Engine's own networks; mirrors the contract so the form refuses them before submitting.
const PREDEFINED_NAMES = new Set(['bridge', 'host', 'none'])

/**
 * Networks section of the Docker Host Explorer (P2): the host's networks live from the Engine, with
 * create and remove. The Engine's predefined `bridge`, `host`, and `none` networks are labelled and
 * cannot be removed (the control is disabled and the API refuses it too). Writes are disabled while
 * the Server is locked.
 */
export function DockerNetworksSection({ serverId, readOnlyReason, onChanged }: DockerSectionProps) {
  const { dockerHosts } = useApp()
  const networks = useAsyncData(() => dockerHosts.listNetworks(serverId), [serverId])
  const reloadNetworks = networks.reload
  const changed = useCallback(() => {
    reloadNetworks()
    onChanged()
  }, [reloadNetworks, onChanged])
  const { busyKey, run } = useDockerMutation(changed)
  const [creating, setCreating] = useState(false)
  const [pendingRemove, setPendingRemove] = useState<DockerNetwork | null>(null)
  const readOnly = readOnlyReason !== undefined

  const remove = async () => {
    if (!pendingRemove) return
    await run(`remove:${pendingRemove.id}`, () => dockerHosts.removeNetwork(serverId, pendingRemove.id), {
      success: 'Network removed',
      failure: 'Network could not be removed',
      target: pendingRemove.name,
    })
    setPendingRemove(null)
  }

  return (
    <SectionSurface
        title="Networks"
        description="Container networks on this host, read live from its Docker Engine."
        actions={
          <HStack gap="2">
            <Button size="sm" colorPalette="brand" onClick={() => setCreating(true)} disabled={readOnly}>
              <Plus size={16} aria-hidden /> Create network
            </Button>
            <Button variant="plain" size="sm" onClick={networks.reload}>
              <RefreshCw size={16} aria-hidden /> Refresh
            </Button>
          </HStack>
        }
      >
      <AsyncSection state={networks} unavailableTitle="Docker explorer is unavailable" unavailableHint={DOCKER_EXPLORER_UNAVAILABLE_HINT}>
        {(items) =>
          items.length === 0 ? (
            <EmptyState title="No networks" message="The Docker Engine reported no networks." />
          ) : (
            <ResponsiveResourceList
              label="Docker networks"
              items={items}
              rowKey={(network) => network.id}
              identityHeader="Network"
              title={(network) => (
                <HStack gap="2" wrap="wrap">
                  <span>{network.name}</span>
                  {network.predefined && <Badge colorPalette="gray" variant="subtle">predefined</Badge>}
                  {network.internal && <Badge colorPalette="purple" variant="subtle">internal</Badge>}
                  {network.attachable && <Badge colorPalette="blue" variant="subtle">attachable</Badge>}
                </HStack>
              )}
              subtitle={(network) => shortDockerId(network.id)}
              columns={[
                { header: 'Driver', cell: (network) => network.driver || '—' },
                { header: 'Scope', cell: (network) => network.scope || '—' },
                {
                  header: 'Subnets',
                  cell: (network) =>
                    network.subnets.length === 0 ? (
                      '—'
                    ) : (
                      <Stack gap="0">
                        {network.subnets.map((subnet) => (
                          <span key={subnet.subnet} className="mono">
                            {subnet.subnet}
                            {subnet.gateway ? ` via ${subnet.gateway}` : ''}
                          </span>
                        ))}
                      </Stack>
                    ),
                },
              ]}
              actions={(network) => (
                <Button
                  size="sm"
                  variant="outline"
                  colorPalette="red"
                  disabled={readOnly || network.predefined || busyKey !== null}
                  loading={busyKey === `remove:${network.id}`}
                  aria-label={
                    network.predefined ? `${network.name} is predefined and cannot be removed` : `Remove network ${network.name}`
                  }
                  onClick={() => setPendingRemove(network)}
                >
                  <Trash2 size={16} aria-hidden /> Remove
                </Button>
              )}
            />
          )
        }
      </AsyncSection>

      {creating && (
        <CreateNetworkDialog
          serverId={serverId}
          onClose={() => setCreating(false)}
          onCreated={() => {
            setCreating(false)
            changed()
          }}
        />
      )}
      <ConfirmDialog
        open={pendingRemove !== null}
        title="Remove network"
        confirmLabel="Remove network"
        busy={pendingRemove !== null && busyKey === `remove:${pendingRemove.id}`}
        onCancel={() => setPendingRemove(null)}
        onConfirm={() => void remove()}
      >
        <Text>
          Removing <strong>{pendingRemove?.name}</strong> deletes the network. A network with attached containers cannot
          be removed; disconnect or remove those containers first.
        </Text>
      </ConfirmDialog>
    </SectionSurface>
  )
}

/**
 * Create-network form: a required name, a driver (default bridge), an optional subnet with an
 * optional gateway, and the internal/attachable flags. Field rules mirror the contract so the
 * obvious mistakes are flagged before submitting; the Engine validates the rest.
 */
function CreateNetworkDialog({ serverId, onClose, onCreated }: { serverId: string; onClose: () => void; onCreated: () => void }) {
  const { dockerHosts } = useApp()
  const { showToast } = useToast()
  const [name, setName] = useState('')
  const [driver, setDriver] = useState('bridge')
  const [subnet, setSubnet] = useState('')
  const [gateway, setGateway] = useState('')
  const [internal, setInternal] = useState(false)
  const [attachable, setAttachable] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const trimmedName = name.trim()
  const nameError =
    trimmedName === ''
      ? ''
      : !DOCKER_OBJECT_NAME_PATTERN.test(trimmedName)
        ? 'Use letters, digits, and _ . - (starting with a letter or digit).'
        : PREDEFINED_NAMES.has(trimmedName)
          ? `${trimmedName} is a predefined network name.`
          : ''
  const gatewayError = gateway.trim() !== '' && subnet.trim() === '' ? 'A gateway needs a subnet.' : ''
  const valid = trimmedName !== '' && nameError === '' && gatewayError === ''

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const network = await dockerHosts.createNetwork(serverId, {
        name: trimmedName,
        driver: driver.trim() || 'bridge',
        internal,
        attachable,
        ...(subnet.trim() ? { subnet: subnet.trim() } : {}),
        ...(gateway.trim() ? { gateway: gateway.trim() } : {}),
      })
      showToast({ tone: 'success', title: 'Network created', description: network.name })
      onCreated()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The network could not be created.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !submitting && onClose()}
      closeOnInteractOutside={!submitting}
      title="Create network"
      description="Containers attached to the same network can reach each other by name."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!valid || submitting}>
            Create
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The network could not be created">
            {error}
          </Alert>
        )}
        <SimpleGrid columns={{ base: 1, md: 2 }} gap="4">
          <Field.Root required invalid={nameError !== ''}>
            <Field.Label>
              Name <Field.RequiredIndicator />
            </Field.Label>
            <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="app-net" autoFocus />
            <Field.ErrorText>{nameError}</Field.ErrorText>
          </Field.Root>
          <Field.Root>
            <Field.Label>Driver</Field.Label>
            <Input value={driver} onChange={(event) => setDriver(event.target.value)} placeholder="bridge" />
          </Field.Root>
          <Field.Root>
            <Field.Label>Subnet</Field.Label>
            <Input value={subnet} onChange={(event) => setSubnet(event.target.value)} placeholder="172.20.0.0/16" className="mono" />
            <Field.HelperText>Optional CIDR; the Engine picks one when empty.</Field.HelperText>
          </Field.Root>
          <Field.Root invalid={gatewayError !== ''}>
            <Field.Label>Gateway</Field.Label>
            <Input value={gateway} onChange={(event) => setGateway(event.target.value)} placeholder="172.20.0.1" className="mono" />
            <Field.ErrorText>{gatewayError}</Field.ErrorText>
          </Field.Root>
        </SimpleGrid>
        <Checkbox checked={internal} onCheckedChange={setInternal}>
          Internal: no access to networks outside this host
        </Checkbox>
        <Checkbox checked={attachable} onCheckedChange={setAttachable}>
          Attachable: containers started later can join it
        </Checkbox>
      </Stack>
    </Modal>
  )
}
