import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Box, Button, Field, HStack, Input, InputGroup, Link, SimpleGrid, Stack, Tabs, Text } from '@chakra-ui/react'
import { ChevronRight, Disc3, HardDrive, MonitorCog, Network, Router, TriangleAlert } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { BootISO, HostEnrollmentBundle } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { autoInspectEnabled, type Integration } from '@/domain/site/types'
import { DeploymentBadge } from '@/presentation/components/AxisBadge'
import { CodeBlock } from '@/presentation/components/CodeBlock'
import { CopyButton } from '@/presentation/components/CopyButton'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { redfishBootISOCommands } from './bootISORedfish'
import { VirtualMachineEnrollmentAction } from './VirtualMachineEnrollmentAction'

/**
 * Where the guide is. The first two steps are questions; the others each show one action and then
 * wait for the Server to appear.
 */
type GuideStep = 'keep-os' | 'dhcp' | 'pxe' | 'boot-iso' | 'existing-os' | 'virtual-machines'

/** The step a Back press returns to. */
const PREVIOUS_STEP: Record<GuideStep, GuideStep | null> = {
  'keep-os': null,
  dhcp: 'keep-os',
  pxe: 'dhcp',
  'boot-iso': 'dhcp',
  'existing-os': 'keep-os',
  'virtual-machines': 'keep-os',
}

interface AddServersDialogProps {
  /** The provisioner Integrations of the current Site scope; the guide offers only these. */
  provisioners: readonly Integration[]
  /** The live Site working set; Servers that appear while the guide is open are followed. */
  servers: readonly Server[]
  onClose: () => void
}

/**
 * Guides an operator through adding Servers (Server Enrollment, decision 053), from the Server
 * list's header and empty state.
 *
 * It asks one question per step and shows only the next action: keep the OS, or PXE boot; for
 * PXE, whether the provisioner's DHCP or an external DHCP serves the network (external DHCP PXE
 * boots through the iPXE Boot ISO, mounted from the BMC console or with Redfish). While the
 * experimental `virtualMachines` switch is on, a third answer enrolls libvirt virtual machines by
 * name from a hypervisor (decision 055). Each path ends on a waiting panel that lists Servers
 * appearing in the live working set since the guide opened, with their state, so the operator sees
 * New → Inspecting → Ready (or Deployed) without leaving. Whether the server is restarted is the
 * operator's call; the guide only says how it must boot. Servers are never created here; they come
 * from reconciliation.
 *
 * The existing-OS command embeds the provisioner's credential. It is read only on that step,
 * kept only in component state, and dropped on close.
 */
export function AddServersDialog({ provisioners, servers, onClose }: AddServersDialogProps) {
  const [step, setStep] = useState<GuideStep>('keep-os')
  const virtualMachines = useExperimentalFeature('virtualMachines')
  // Servers present when the guide opened; anything else in the live working set is new.
  const [knownIds] = useState(() => new Set(servers.map((server) => server.id)))
  const appeared = useMemo(
    () => servers.filter((server) => !knownIds.has(server.id) && !server.absent),
    [knownIds, servers],
  )
  const previous = PREVIOUS_STEP[step]
  return (
    <Modal
      open
      onClose={onClose}
      title="Add servers"
      footer={
        <HStack gap="2" justify="space-between" width="full">
          <Button variant="ghost" onClick={() => previous && setStep(previous)} visibility={previous ? 'visible' : 'hidden'}>
            Back
          </Button>
          <Button variant="outline" onClick={onClose}>
            {appeared.length > 0 ? 'Done' : 'Close'}
          </Button>
        </HStack>
      }
    >
      {step === 'keep-os' && (
        <Question title="Does the server already run an OS you want to keep?">
          <Choice icon={<Network size={20} />} title="No, boot it from PXE" hint="Swallow then inspects its hardware" onSelect={() => setStep('dhcp')} />
          <Choice icon={<HardDrive size={20} />} title="Yes, keep its OS" hint="Run one command on the server" onSelect={() => setStep('existing-os')} />
          {virtualMachines && (
            <Choice
              icon={<MonitorCog size={20} />}
              title="It is a libvirt virtual machine"
              hint="Choose it by name on its hypervisor"
              onSelect={() => setStep('virtual-machines')}
            />
          )}
        </Question>
      )}
      {step === 'dhcp' && (
        <Question title="Which DHCP serves its PXE network?">
          <Choice icon={<Router size={20} />} title="Provisioner DHCP" hint="PXE boots directly" onSelect={() => setStep('pxe')} />
          <Choice icon={<Disc3 size={20} />} title="External DHCP" hint="PXE boots through the iPXE Boot ISO" onSelect={() => setStep('boot-iso')} />
        </Question>
      )}
      {step === 'pxe' && (
        <Stack gap="4">
          <Action>Boot the server from PXE.</Action>
          <Waiting servers={appeared} provisioners={provisioners} inspects />
        </Stack>
      )}
      {step === 'boot-iso' && (
        <Stack gap="4">
          <BootISOAction provisioners={provisioners} />
          <Waiting servers={appeared} provisioners={provisioners} inspects enableBootMedia />
        </Stack>
      )}
      {step === 'existing-os' && (
        <Stack gap="4">
          <ExistingOSAction provisioners={provisioners} />
          <Waiting servers={appeared} provisioners={provisioners} />
        </Stack>
      )}
      {step === 'virtual-machines' && virtualMachines && (
        <Stack gap="4">
          <VirtualMachineEnrollmentAction provisioners={provisioners} servers={servers} />
          <Waiting servers={appeared} provisioners={provisioners} inspects />
        </Stack>
      )}
    </Modal>
  )
}

/** A question step: the question as a heading over its answers. */
function Question({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Stack gap="3" role="group" aria-label={title}>
      <Text fontWeight="semibold">{title}</Text>
      {children}
    </Stack>
  )
}

/** One answer: a full-width button whose accessible name is its title and hint. */
function Choice({ icon, title, hint, onSelect }: { icon: ReactNode; title: string; hint: string; onSelect: () => void }) {
  return (
    <Button variant="outline" h="auto" py="3" px="4" justifyContent="flex-start" textAlign="start" onClick={onSelect}>
      <Box color="brand.fg" aria-hidden>
        {icon}
      </Box>
      <Stack gap="0" flex="1" alignItems="flex-start">
        <Text fontWeight="semibold">{title}</Text>
        <Text fontSize="sm" color="fg.muted" fontWeight="normal">
          {hint}
        </Text>
      </Stack>
      <ChevronRight size={16} aria-hidden />
    </Button>
  )
}

/** The one thing the operator does on an action step. */
function Action({ children }: { children: ReactNode }) {
  return <Text fontWeight="medium">{children}</Text>
}

/**
 * The external-DHCP step: pick (or build) the provisioner's Boot ISO, then mount it on the
 * server's BMC — from the BMC web console, or with the Redfish commands shown for the BMC address
 * and user the operator types (curl prompts for the password; it is never entered here). Boot
 * ISOs are read for the current Site when the step opens; a failed read is shown inline.
 */
function BootISOAction({ provisioners }: { provisioners: readonly Integration[] }) {
  const { provisioning } = useApp()
  const { siteId, scopedHref } = useSiteScope()
  const [isos, setIsos] = useState<{ status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; items: BootISO[] }>({ status: 'loading' })
  const [isoId, setIsoId] = useState('')
  const [bmc, setBmc] = useState({ address: '', user: '' })

  // Reads the Site's Boot ISOs once per Site; a late answer for a closed dialog is discarded.
  useEffect(() => {
    let cancelled = false
    const ids = new Set(provisioners.map((integration) => integration.id))
    provisioning
      .listBootISOs({ siteId })
      .then((catalog) => {
        if (cancelled) return
        const items = catalog.items.filter((iso) => ids.has(iso.integrationId) && iso.url)
        setIsos({ status: 'ready', items })
        setIsoId((current) => current || items[0]?.id || '')
      })
      .catch((error: unknown) => {
        if (!cancelled) setIsos({ status: 'error', message: error instanceof Error ? error.message : 'Boot ISOs could not be read.' })
      })
    return () => {
      cancelled = true
    }
  }, [provisioning, provisioners, siteId])

  if (isos.status === 'loading') return <Text color="fg.muted">Loading Boot ISOs…</Text>
  if (isos.status === 'error') {
    return (
      <Alert status="error" title="Boot ISOs could not be read">
        {isos.message}
      </Alert>
    )
  }
  if (isos.items.length === 0) {
    return (
      <Stack gap="2" alignItems="flex-start">
        <Action>Build a Boot ISO for the provisioner first.</Action>
        <Button asChild colorPalette="brand" size="sm">
          <RouterLink to={scopedHref('/provisioning/boot-isos')}>Build Boot ISO</RouterLink>
        </Button>
      </Stack>
    )
  }
  const iso = isos.items.find((item) => item.id === isoId) ?? isos.items[0]
  const owner = (id: string) => provisioners.find((integration) => integration.id === id)?.name ?? id
  const commands = redfishBootISOCommands({ bmcAddress: bmc.address, bmcUser: bmc.user, isoUrl: iso.url })
  return (
    <Stack gap="3">
      <Action>Mount the Boot ISO on the server&apos;s BMC as virtual media, then boot from it.</Action>
      {isos.items.length > 1 && (
        <Select
          id="add-servers-boot-iso"
          aria-label="Boot ISO"
          value={iso.id}
          onChange={setIsoId}
          options={isos.items.map((item) => ({ value: item.id, label: `${item.name} (${owner(item.integrationId)})` }))}
        />
      )}
      <InputGroup endElement={<CopyButton value={iso.url} label="Copy Boot ISO URL" />} endElementProps={{ pe: '1' }}>
        <Input aria-label="Boot ISO URL" value={iso.url} readOnly className="sw-mono" fontSize="sm" spellCheck={false} />
      </InputGroup>
      <Tabs.Root defaultValue="console" size="sm" variant="line" aria-label="How to mount the Boot ISO">
        <Tabs.List>
          <Tabs.Trigger value="console">BMC console</Tabs.Trigger>
          <Tabs.Trigger value="redfish">Redfish</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="console">
          <Text fontSize="sm" color="fg.muted">
            Virtual Media → mount the URL as a CD → boot once from the virtual CD.
          </Text>
        </Tabs.Content>
        <Tabs.Content value="redfish">
          <Stack gap="2">
            <SimpleGrid columns={2} gap="2">
              <Input
                aria-label="BMC address"
                placeholder="BMC address"
                size="sm"
                value={bmc.address}
                autoComplete="off"
                spellCheck={false}
                onChange={(event) => setBmc((current) => ({ ...current, address: event.target.value }))}
              />
              <Input
                aria-label="BMC user"
                placeholder="admin"
                size="sm"
                value={bmc.user}
                autoComplete="off"
                spellCheck={false}
                onChange={(event) => setBmc((current) => ({ ...current, user: event.target.value }))}
              />
            </SimpleGrid>
            <HStack justify="space-between">
              <Text fontSize="sm" color="fg.muted">
                Run from a machine that reaches the BMC.
              </Text>
              <CopyButton value={commands} label="Copy Redfish commands" />
            </HStack>
            <CodeBlock code={commands} language="bash" aria-label="Redfish commands" />
          </Stack>
        </Tabs.Content>
      </Tabs.Root>
    </Stack>
  )
}

/** The enrollment bundle read for one provisioner. */
type BundleState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; bundle: HostEnrollmentBundle }

/**
 * The existing-OS step: one line to run as root on the host. It downloads the enrollment script
 * from this console's own origin, and the script downloads the swallow CLI from there too, so the
 * host needs nothing installed beforehand. The bundle is read as soon as a provisioner is known
 * (immediately with one provisioner), because choosing this path is the operator's request for it.
 */
function ExistingOSAction({ provisioners }: { provisioners: readonly Integration[] }) {
  const { provisioning } = useApp()
  const [integrationId, setIntegrationId] = useState(provisioners.length === 1 ? provisioners[0].id : '')
  const [bundle, setBundle] = useState<{ integrationId: string; state: BundleState } | null>(null)

  // Reads the bundle for the chosen provisioner; switching provisioners discards the previous one so
  // its credential is never shown under another name. The host reaches swallow where this console does.
  useEffect(() => {
    if (!integrationId) return
    let cancelled = false
    provisioning
      .getHostEnrollmentBundle(integrationId, window.location.origin)
      .then((value) => {
        if (!cancelled) setBundle({ integrationId, state: { status: 'ready', bundle: value } })
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setBundle({ integrationId, state: { status: 'error', message: error instanceof Error ? error.message : 'The command could not be generated.' } })
        }
      })
    return () => {
      cancelled = true
    }
  }, [integrationId, provisioning])

  if (provisioners.length === 0) {
    return <Alert status="info" title="No provisioner in this Site" />
  }
  const state: BundleState | null = !integrationId
    ? null
    : bundle?.integrationId === integrationId
      ? bundle.state
      : { status: 'loading' }

  return (
    <Stack gap="3">
      {provisioners.length > 1 && (
        <Field.Root>
          <Field.Label htmlFor="add-servers-provisioner">Provisioner</Field.Label>
          <Select
            id="add-servers-provisioner"
            aria-label="Provisioner"
            placeholder="Select a provisioner"
            value={integrationId}
            onChange={setIntegrationId}
            options={provisioners.map((integration) => ({ value: integration.id, label: integration.name }))}
          />
        </Field.Root>
      )}
      {state?.status === 'loading' && <Text color="fg.muted">Generating the command…</Text>}
      {state?.status === 'error' && (
        <Alert status="error" title="The command could not be generated">
          {state.message}
        </Alert>
      )}
      {state?.status === 'ready' && (
        <>
          <HStack justify="space-between">
            <Action>Run this on the server.</Action>
            <CopyButton value={state.bundle.command} label="Copy command" />
          </HStack>
          <CodeBlock code={state.bundle.command} language="bash" aria-label="Enrollment command" />
          <HStack gap="1.5" color="fg.warning" fontSize="sm">
            <TriangleAlert size={14} aria-hidden />
            <Text>Contains the provisioner&apos;s API key.</Text>
          </HStack>
        </>
      )}
    </Stack>
  )
}

interface WaitingProps {
  /** Servers that appeared since the guide opened. */
  servers: readonly Server[]
  provisioners: readonly Integration[]
  /** The path ends in automatic inspection, so Servers are expected to go New → Ready. */
  inspects?: boolean
  /** Offer Enable Boot Media for each Server, for the Boot ISO path. */
  enableBootMedia?: boolean
}

/**
 * The live end of every path: a spinner until a Server appears, then each new Server with its
 * state, so the guide shows that the step worked. The list is announced politely as it changes.
 * When the path relies on automatic inspection and a provisioner has it off, one line says to run
 * Inspect hardware instead.
 */
function Waiting({ servers, provisioners, inspects = false, enableBootMedia = false }: WaitingProps) {
  const { scopedHref } = useSiteScope()
  const manual = inspects ? provisioners.filter((integration) => !autoInspectEnabled(integration)) : []
  return (
    <Box borderTopWidth="1px" borderColor="border.muted" pt="3" aria-live="polite">
      {servers.length === 0 ? (
        <HStack gap="2" color="fg.muted">
          <InProgressSpinner />
          <Text>Waiting for the server to appear…</Text>
        </HStack>
      ) : (
        <Stack gap="2">
          {servers.map((server) => (
            <HStack key={server.id} justify="space-between" gap="3">
              <Link asChild colorPalette="brand" fontWeight="medium">
                <RouterLink to={scopedHref(`/servers/${server.id}/summary`)}>{serverDisplayName(server)}</RouterLink>
              </Link>
              <HStack gap="3">
                <DeploymentBadge axis={server.deployment} provider={server.provisioning} />
                {enableBootMedia && (
                  <Link asChild colorPalette="brand" fontSize="sm">
                    <RouterLink to={scopedHref(`/servers/${server.id}/summary`)}>Enable Boot Media</RouterLink>
                  </Link>
                )}
              </HStack>
            </HStack>
          ))}
        </Stack>
      )}
      {manual.length > 0 && (
        <Text fontSize="sm" color="fg.muted" mt="2">
          Automatic inspection is off for {manual.map((integration) => integration.name).join(', ')}: run Inspect hardware when it appears.
        </Text>
      )}
    </Box>
  )
}
