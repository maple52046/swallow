import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Checkbox,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
  ToggleGroup,
  ToggleGroupItem,
} from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useApp } from '@/di/AppProvider'
import type {
  ManualNetworkMode,
  NetworkInterface,
  NetworkLink,
  NetworkTarget,
} from '@/domain/provisioning/types'
import { DetailTableView, DetailUnavailable } from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useServerDetailContext } from './useServerDetail'

/** Shared provider-detail table tab used by Storage and PCI routes. */
export function ServerDetailTableTab({ title }: { title: string }) {
  const { detail, detailError } = useServerDetailContext()
  if (!detail) return <DetailUnavailable message={detailError ?? 'The provisioner did not return detail.'} />
  const table = findTable(detail.tables, title)
  if (!table) return <DetailUnavailable message={`This provisioner reported no ${title.toLowerCase()} detail.`} />
  return <div className="sw-section"><SectionHeader title={table.title} /><StickyTableFrame><DetailTableView table={table} /></StickyTableFrame></div>
}

type NetworkState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; target: NetworkTarget }

interface LinkEditor {
  iface: NetworkInterface
  link?: NetworkLink
}

function configurationLabel(state: string, rawMode: string): string {
  if (state === 'provider_managed') return rawMode ? `Provider-managed (${rawMode === 'AUTO' ? 'MAAS AUTO' : rawMode})` : 'Provider-managed'
  if (state === 'link_only') return 'Link only'
  if (state === 'unconfigured') return 'Unconfigured'
  return state ? state.toUpperCase() : 'Unknown'
}


function validIPv4(value: string): boolean {
  const octets = value.trim().split('.')
  return octets.length === 4 && octets.every((octet) => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255)
}
/** Typed NIC and subnet-link management owned by Swallow's provisioning contract. */
export function ServerNetworkTab() {
  const { server } = useServerDetailContext()
  const { servers } = useApp()
  const { showToast } = useToast()
  const [state, setState] = useState<NetworkState>({ status: 'loading' })
  const [editor, setEditor] = useState<LinkEditor | null>(null)
  const [unbind, setUnbind] = useState<LinkEditor | null>(null)
  const [mode, setMode] = useState<ManualNetworkMode>('dhcp')
  const [subnetId, setSubnetId] = useState('')
  const [ipAddress, setIPAddress] = useState('')
  const [defaultGateway, setDefaultGateway] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      setState({ status: 'ready', target: await servers.getNetwork(server.id) })
    } catch (error) {
      setState({ status: 'error', message: error instanceof Error ? error.message : 'Could not load network configuration.' })
    }
  }, [server.id, servers])

  useEffect(() => { void load() }, [load])

  const openEditor = (iface: NetworkInterface, link?: NetworkLink) => {
    const writableMode: ManualNetworkMode = link?.configurationState === 'static'
      ? 'static'
      : link?.configurationState === 'link_only' ? 'link_only' : 'dhcp'
    setMode(writableMode)
    setSubnetId(link?.subnetId || (iface.availableSubnets.length === 1 ? iface.availableSubnets[0].id : ''))
    setIPAddress(link?.configurationState === 'static' ? link.ipAddress : '')
    setDefaultGateway(link?.defaultGateway ?? false)
    setEditor({ iface, link })
  }

  const validStaticIP = mode !== 'static' || validIPv4(ipAddress)
  const validEditor = Boolean(editor && subnetId && validStaticIP)
  const saveLink = async () => {
    if (!editor || !validEditor || saving) return
    setSaving(true)
    try {
      const input = { mode, subnetId, ipAddress: mode === 'static' ? ipAddress.trim() : undefined, defaultGateway: mode !== 'link_only' && defaultGateway }
      const target = editor.link
        ? await servers.replaceNetworkLink(server.id, editor.iface.id, editor.link.id, input)
        : await servers.createNetworkLink(server.id, editor.iface.id, input)
      setState({ status: 'ready', target })
      setEditor(null)
      showToast({ tone: 'success', title: 'Network configuration updated' })
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not update network configuration', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setSaving(false)
    }
  }

  const unlink = async () => {
    if (!unbind?.link || saving) return
    setSaving(true)
    try {
      const target = await servers.deleteNetworkLink(server.id, unbind.iface.id, unbind.link.id)
      setState({ status: 'ready', target })
      setUnbind(null)
      showToast({ tone: 'success', title: 'Network link removed' })
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not remove network link', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setSaving(false)
    }
  }

  const rows = useMemo<Array<{ iface: NetworkInterface; link?: NetworkLink }>>(() => {
    if (state.status !== 'ready') return []
    return state.target.network.interfaces.flatMap<{ iface: NetworkInterface; link?: NetworkLink }>((iface) => iface.links.length > 0
      ? iface.links.map((link) => ({ iface, link }))
      : [{ iface, link: undefined }])
  }, [state])

  if (state.status === 'loading') return <LoadingState rows={5} />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={() => void load()} />

  return <div className="sw-activity-stack">
    <section className="sw-section">
      <SectionHeader
        title="Network interfaces"
        description="Address configuration and physical link state are reported separately. Changes are available only while the Server is Ready and unlocked."
        actions={<Button variant="secondary" onClick={() => void load()}>Refresh</Button>}
      />
      {!state.target.editable && <Alert variant={AlertVariant.info} title="Network configuration is read-only" isInline>{state.target.disabledReason}</Alert>}
      {rows.length === 0 ? <EmptyState title="No network interfaces" message="The provisioner did not report any NICs for this Server." /> : (
        <StickyTableFrame>
          <Table aria-label="Server network interfaces" variant="compact">
            <Thead><Tr><Th>Interface</Th><Th>MAC address</Th><Th>Boot NIC</Th><Th>Physical link</Th><Th>Configuration</Th><Th>Subnet</Th><Th>IP address</Th><Th>Gateway</Th><Th>Provider mode</Th><Th screenReaderText="Actions" /></Tr></Thead>
            <Tbody>{rows.map(({ iface, link }, index) => <Tr key={`${iface.id}:${link?.id ?? index}`}>
              <Td dataLabel="Interface"><strong>{iface.name || '-'}</strong></Td>
              <Td dataLabel="MAC address" className="sw-mono">{iface.macAddress || '-'}</Td>
              <Td dataLabel="Boot NIC">{iface.boot ? <Label color="blue">Boot</Label> : '-'}</Td>
              <Td dataLabel="Physical link">{iface.physicalState || 'unknown'}</Td>
              <Td dataLabel="Configuration">{configurationLabel(link?.configurationState ?? iface.configurationState, link?.rawProviderMode ?? iface.rawProviderMode)}</Td>
              <Td dataLabel="Subnet">{link?.subnetName || link?.cidr || '-'}</Td>
              <Td dataLabel="IP address" className="sw-mono">{link?.ipAddress || '-'}</Td>
              <Td dataLabel="Gateway">{link?.defaultGateway ? 'Default' : '-'}</Td>
              <Td dataLabel="Provider mode" className="sw-mono">{link?.rawProviderMode || iface.rawProviderMode || '-'}</Td>
              <Td isActionCell><span className="sw-row-actions">
                <Button variant="link" isInline isDisabled={!state.target.editable} onClick={() => openEditor(iface, link)}>{link ? 'Configure' : 'Add link'}</Button>
                {link && <Button variant="link" isDanger isInline isDisabled={!state.target.editable} onClick={() => setUnbind({ iface, link })}>Unbind</Button>}
              </span></Td>
            </Tr>)}</Tbody>
          </Table>
        </StickyTableFrame>
      )}
    </section>

    {editor && <Modal isOpen onClose={() => !saving && setEditor(null)} variant="small" aria-labelledby="network-editor-title">
      <ModalHeader title={editor.link ? `Configure ${editor.iface.name}` : `Add link to ${editor.iface.name}`} labelId="network-editor-title" description="Swallow replaces only the selected link and leaves every other NIC and subnet link untouched." />
      <ModalBody>
        <Form>
          <FormGroup label="Mode" isRequired fieldId="network-mode">
            <ToggleGroup aria-label="Network configuration mode">
              <ToggleGroupItem text="DHCP" buttonId="network-mode-dhcp" isSelected={mode === 'dhcp'} onChange={() => { setMode('dhcp'); setIPAddress(''); setDefaultGateway(false) }} />
              <ToggleGroupItem text="Static" buttonId="network-mode-static" isSelected={mode === 'static'} onChange={() => setMode('static')} />
              <ToggleGroupItem text="Link only" buttonId="network-mode-link-only" isSelected={mode === 'link_only'} onChange={() => { setMode('link_only'); setIPAddress(''); setDefaultGateway(false) }} />
            </ToggleGroup>
          </FormGroup>
          <FormGroup label="Subnet" isRequired fieldId="network-subnet">
            <FormSelect id="network-subnet" value={subnetId} onChange={(_event, value) => setSubnetId(value)}>
              <FormSelectOption value="" label="Select a subnet" isDisabled isPlaceholder />
              {editor.iface.availableSubnets.map((subnet) => <FormSelectOption key={subnet.id} value={subnet.id} label={formatSubnetOptionLabel(subnet)} />)}
            </FormSelect>
          </FormGroup>
          {mode === 'static' && <FormGroup label="IPv4 address" isRequired fieldId="network-ip-address">
            <TextInput
              id="network-ip-address"
              value={ipAddress}
              onChange={(_event, value) => setIPAddress(value)}
              placeholder="192.0.2.10"
              validated={!ipAddress || validStaticIP ? 'default' : 'error'}
            />
            <FormHelperText>
              <HelperText>
                <HelperTextItem variant={!ipAddress || validStaticIP ? 'default' : 'error'}>Enter a valid IPv4 address.</HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>}
          {mode === 'static' && <FormGroup fieldId="network-default-gateway"><Checkbox id="network-default-gateway" label="Use this subnet for the default route" description="The provider uses the gateway address configured on this subnet for the Server's IPv4 default route." isChecked={defaultGateway} onChange={(_event, checked) => setDefaultGateway(checked)} /></FormGroup>}
        </Form>
      </ModalBody>
      <ModalFooter><Button variant="primary" isLoading={saving} isDisabled={!validEditor || saving} onClick={() => void saveLink()}>Save configuration</Button><Button variant="link" isDisabled={saving} onClick={() => setEditor(null)}>Cancel</Button></ModalFooter>
    </Modal>}

    {unbind?.link && <Modal isOpen onClose={() => !saving && setUnbind(null)} variant="small" aria-labelledby="network-unbind-title">
      <ModalHeader title="Unbind network link" labelId="network-unbind-title" description={`Remove ${unbind.link.ipAddress || unbind.link.subnetName || 'this subnet link'} from ${unbind.iface.name}. Other links are preserved.`} />
      <ModalBody><Alert variant={AlertVariant.warning} title="The interface will lose this configuration" isInline>Unbind is available only while the Server is Ready and unlocked.</Alert></ModalBody>
      <ModalFooter><Button variant="danger" isLoading={saving} onClick={() => void unlink()}>Unbind</Button><Button variant="link" isDisabled={saving} onClick={() => setUnbind(null)}>Cancel</Button></ModalFooter>
    </Modal>}
  </div>
}

/** Live block-device table. */
export function ServerStorageTab() { return <ServerDetailTableTab title="Storage" /> }
/** Live PCI hardware map. */
export function ServerPciTab() { return <ServerDetailTableTab title="PCI devices" /> }
