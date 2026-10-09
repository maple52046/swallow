import { useMemo, useRef, useState } from 'react'
import { Box, Button, Field, HStack, Input, Link, Stack, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName, type HypervisorVirtualMachines, type Server } from '@/domain/server/types'
import type { Integration } from '@/domain/site/types'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'

/** The hypervisor's domains as read on request; `idle` until the operator asks. */
type ListState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: HypervisorVirtualMachines }

/** The enrollment request: not sent, in flight, refused (with the API's reason), or accepted. */
type SubmitState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'error'; message: string }
  | { status: 'accepted'; workflowId: string; count: number }

interface VirtualMachineEnrollmentActionProps {
  /** The provisioner Integrations of the current Site scope; the virtual machines enroll into one of them. */
  provisioners: readonly Integration[]
  /** The live Site working set; its deployed Servers are the hypervisors offered. */
  servers: readonly Server[]
}

/**
 * The libvirt virtual-machine path of Add servers (contract server-enrollment.md "Virtual machines
 * (libvirt)", decision 055), shown only while the experimental `virtualMachines` switch is on.
 *
 * The operator picks a hypervisor — a deployed Server of this Site, which the API logs in to with
 * the Deployment Key as its Server Default User or the account typed here — lists its domains on
 * request (a live SSH read that takes seconds), ticks the ones to enroll by name, and optionally
 * chooses a Boot ISO for a network the provisioner's DHCP does not serve. Domains that already are
 * Servers (one of their MAC addresses matches) link to that Server and cannot be ticked. Enrolling
 * starts one enroll-virtual-machines Workflow and links to it: its per-domain Tasks run in the
 * worker, and a Task that needs the operator (a running domain, a hypervisor the provisioner cannot
 * reach) says so there. The new Servers then appear in the waiting list below and go through
 * automatic inspection. Nothing is typed as a MAC address, and no credential passes through here.
 */
export function VirtualMachineEnrollmentAction({ provisioners, servers }: VirtualMachineEnrollmentActionProps) {
  const { servers: serverRepository, provisioning } = useApp()
  const { scopedHref } = useSiteScope()
  const [integrationId, setIntegrationId] = useState(provisioners.length === 1 ? provisioners[0].id : '')
  const [hypervisorId, setHypervisorId] = useState('')
  const [account, setAccount] = useState('')
  const [list, setList] = useState<ListState>({ status: 'idle' })
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [bootIsoId, setBootIsoId] = useState('')
  const [powerOffRunning, setPowerOffRunning] = useState(false)
  const [submit, setSubmit] = useState<SubmitState>({ status: 'idle' })
  // Identifies the newest list request, so a slow answer for a previous hypervisor or account never
  // replaces the current one.
  const listRequest = useRef(0)

  const hypervisors = useMemo(
    () => servers.filter((server) => !server.absent && server.provisioning?.state === 'deployed' && server.addresses.length > 0),
    [servers],
  )
  const hypervisor = hypervisors.find((server) => server.id === hypervisorId)
  const bootISOs = useAsyncData(
    async () => (integrationId ? (await provisioning.listBootISOs({ integrationId })).items : []),
    [provisioning, integrationId],
  )

  if (provisioners.length === 0) {
    return <Alert status="info" title="No provisioner in this Site" />
  }
  if (hypervisors.length === 0) {
    return (
      <Alert status="info" title="No hypervisor to read">
        A hypervisor is a deployed Server of this Site that swallow logs in to with the Deployment Key. Deploy or enroll it
        first, then come back.
      </Alert>
    )
  }

  const chooseHypervisor = (id: string) => {
    setHypervisorId(id)
    setList({ status: 'idle' })
    setSelected(new Set())
    setSubmit({ status: 'idle' })
  }

  const loadVirtualMachines = async () => {
    if (!hypervisorId) return
    const request = ++listRequest.current
    setList({ status: 'loading' })
    setSelected(new Set())
    setSubmit({ status: 'idle' })
    try {
      const data = await serverRepository.listVirtualMachines(hypervisorId, account)
      if (request === listRequest.current) setList({ status: 'ready', data })
    } catch (caught) {
      if (request === listRequest.current) {
        setList({ status: 'error', message: caught instanceof Error ? caught.message : 'The virtual machines could not be read.' })
      }
    }
  }

  const toggle = (name: string, checked: boolean) => {
    setSelected((current) => {
      const next = new Set(current)
      if (checked) next.add(name)
      else next.delete(name)
      return next
    })
  }

  const enroll = async () => {
    if (!integrationId || !hypervisorId || selected.size === 0 || list.status !== 'ready') return
    setSubmit({ status: 'submitting' })
    try {
      const accepted = await provisioning.enrollVirtualMachines({
        integrationId,
        hypervisorServerId: hypervisorId,
        domains: list.data.items.filter((item) => selected.has(item.name)).map((item) => item.name),
        bootIsoId: bootIsoId || undefined,
        // The account the list was read as, so the enrollment logs in the same way.
        account: account.trim() || undefined,
        powerOffRunning,
      })
      setSubmit({ status: 'accepted', workflowId: accepted.workflowId, count: selected.size })
    } catch (caught) {
      setSubmit({ status: 'error', message: caught instanceof Error ? caught.message : 'The enrollment could not be started.' })
    }
  }

  const isoOptions = bootISOs.status === 'ready' ? bootISOs.data : []
  const accepted = submit.status === 'accepted'
  return (
    <Stack gap="4">
      <Text fontWeight="medium">Choose virtual machines on their hypervisor by name. swallow registers them, and inspection boots them.</Text>
      {provisioners.length > 1 && (
        <Field.Root required>
          <Field.Label htmlFor="add-vms-provisioner">Provisioner</Field.Label>
          <Select
            id="add-vms-provisioner"
            aria-label="Provisioner"
            placeholder="Select a provisioner"
            value={integrationId}
            disabled={accepted}
            onChange={(id) => {
              setIntegrationId(id)
              setBootIsoId('')
            }}
            options={provisioners.map((integration) => ({ value: integration.id, label: integration.name }))}
          />
        </Field.Root>
      )}
      <Field.Root required>
        <Field.Label htmlFor="add-vms-hypervisor">Hypervisor</Field.Label>
        <Select
          id="add-vms-hypervisor"
          aria-label="Hypervisor"
          placeholder="Select a deployed Server"
          value={hypervisorId}
          disabled={accepted}
          onChange={chooseHypervisor}
          options={hypervisors.map((server) => ({ value: server.id, label: `${serverDisplayName(server)} (${server.addresses[0]})` }))}
        />
        <Field.HelperText>A deployed Server swallow logs in to with the Deployment Key.</Field.HelperText>
      </Field.Root>
      <Field.Root>
        <Field.Label htmlFor="add-vms-account">Login account</Field.Label>
        <Input
          id="add-vms-account"
          value={account}
          placeholder={hypervisor?.defaultUser?.user ?? 'Server Default User'}
          autoComplete="off"
          spellCheck={false}
          disabled={accepted}
          onChange={(event) => setAccount(event.target.value)}
        />
        <Field.HelperText>Leave empty for the hypervisor&apos;s Server Default User. It must be allowed to run virsh (the libvirt group).</Field.HelperText>
      </Field.Root>
      <HStack>
        <Button size="sm" variant="outline" disabled={!hypervisorId || accepted} loading={list.status === 'loading'} onClick={() => void loadVirtualMachines()}>
          {list.status === 'ready' ? 'Read again' : 'List virtual machines'}
        </Button>
      </HStack>
      {list.status === 'error' && (
        <Alert status="error" title="The virtual machines could not be read">
          {list.message}
        </Alert>
      )}
      {list.status === 'ready' && (
        <VirtualMachineChoices
          list={list.data}
          servers={servers}
          selected={selected}
          disabled={accepted || submit.status === 'submitting'}
          serverHref={(id) => scopedHref(`/servers/${id}/summary`)}
          onToggle={toggle}
        />
      )}
      {list.status === 'ready' && list.data.items.length > 0 && (
        <>
          <Field.Root>
            <Field.Label htmlFor="add-vms-boot-iso">Boot ISO</Field.Label>
            <Select
              id="add-vms-boot-iso"
              aria-label="Boot ISO"
              value={bootIsoId}
              disabled={accepted || bootISOs.status !== 'ready'}
              onChange={setBootIsoId}
              options={[
                { value: '', label: 'None — the provisioner’s DHCP serves this network' },
                ...isoOptions.map((iso) => ({ value: iso.id, label: `${iso.name} — chains to ${iso.chainUrl}` })),
              ]}
            />
            <Field.HelperText>
              {bootISOs.status === 'error'
                ? `Boot ISOs could not be read: ${bootISOs.message}`
                : 'With a Boot ISO, each virtual machine boots it first from its CD-ROM, and keeps that for every inspection and OS deployment.'}
            </Field.HelperText>
          </Field.Root>
          <Checkbox checked={powerOffRunning} disabled={accepted} onCheckedChange={setPowerOffRunning}>
            Stop a running virtual machine (a hard power-off) instead of asking first
          </Checkbox>
          {submit.status === 'error' && (
            <Alert status="error" title="The enrollment was not started">
              {submit.message}
            </Alert>
          )}
          {submit.status === 'accepted' ? (
            <Alert status="success" title={`Enrolling ${submit.count} virtual machine${submit.count === 1 ? '' : 's'}`}>
              <Stack gap="1">
                <Text>Each one is registered and then inspected; they appear below as they arrive.</Text>
                <Link asChild colorPalette="brand" fontWeight="medium">
                  <RouterLink to={scopedHref(`/workflows/${submit.workflowId}`)}>View workflow</RouterLink>
                </Link>
              </Stack>
            </Alert>
          ) : (
            <HStack>
              <Button
                colorPalette="brand"
                size="sm"
                disabled={!integrationId || selected.size === 0}
                loading={submit.status === 'submitting'}
                onClick={() => void enroll()}
              >
                {selected.size > 0 ? `Enroll ${selected.size} virtual machine${selected.size === 1 ? '' : 's'}` : 'Enroll virtual machines'}
              </Button>
            </HStack>
          )}
        </>
      )}
    </Stack>
  )
}

interface VirtualMachineChoicesProps {
  list: HypervisorVirtualMachines
  servers: readonly Server[]
  selected: ReadonlySet<string>
  disabled: boolean
  serverHref: (id: string) => string
  onToggle: (name: string, checked: boolean) => void
}

/**
 * The hypervisor's domains as a checklist named by domain. Each row says libvirt's own state, the
 * architecture, and the MAC addresses swallow will register, so the operator can tell lab VMs
 * apart without opening virt-manager. A domain that already is a Server is shown with a link to it
 * and cannot be chosen again. The list is a labelled group so its checkboxes are announced together.
 */
function VirtualMachineChoices({ list, servers, selected, disabled, serverHref, onToggle }: VirtualMachineChoicesProps) {
  if (list.items.length === 0) {
    return <Text color="fg.muted">No virtual machines on this hypervisor (read as {list.account}).</Text>
  }
  return (
    <Stack gap="2" role="group" aria-label="Virtual machines">
      <Text fontSize="sm" color="fg.muted">Read as {list.account}.</Text>
      {list.items.map((machine) => {
        const existing = machine.serverId ? servers.find((server) => server.id === machine.serverId) : undefined
        return (
          <Box key={machine.name} borderWidth="1px" borderColor="border.muted" rounded="md" px="3" py="2">
            <HStack justify="space-between" gap="3" wrap="wrap">
              <Checkbox
                checked={selected.has(machine.name)}
                disabled={disabled || machine.serverId !== null}
                onCheckedChange={(checked) => onToggle(machine.name, checked)}
              >
                <Text as="span" fontWeight="medium" className="sw-mono">{machine.name}</Text>
              </Checkbox>
              <HStack gap="2">
                <StatusBadge status={machine.state === 'running' ? 'running' : 'unknown'} label={machine.state || 'unknown'} />
                {machine.serverId && (
                  <Link asChild colorPalette="brand" fontSize="sm">
                    <RouterLink to={serverHref(machine.serverId)}>
                      Already {existing ? serverDisplayName(existing) : 'a Server'}
                    </RouterLink>
                  </Link>
                )}
              </HStack>
            </HStack>
            <Text fontSize="sm" color="fg.muted" ps="6" wordBreak="break-all">
              {[machine.architecture, machine.macAddresses.join(', ') || 'no network interface'].filter(Boolean).join(' · ')}
            </Text>
          </Box>
        )
      })}
    </Stack>
  )
}
