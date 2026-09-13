import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Button, Field, Input, SegmentGroup, Stack, Table, VisuallyHidden } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ManualNetworkMode, NetworkInterface, NetworkLink, NetworkTarget } from '@/domain/provisioning/types'
import { DetailTableView, DetailUnavailable } from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useServerDetailContext } from './useServerDetail'

/** Shared provider-detail table tab used by Storage and PCI routes. */
export function ServerDetailTableTab({ title }: { title: string }) {
  const { detail, detailError } = useServerDetailContext()
  if (!detail) return <DetailUnavailable message={detailError ?? 'The provisioner did not return detail.'} />
  const table = findTable(detail.tables, title)
  if (!table) return <DetailUnavailable message={`This provisioner reported no ${title.toLowerCase()} detail.`} />
  return (
    <div className="sw-section">
      <SectionHeader title={table.title} />
      <StickyTableFrame>
        <DetailTableView table={table} />
      </StickyTableFrame>
    </div>
  )
}

type NetworkState = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; target: NetworkTarget }

interface LinkEditor {
  iface: NetworkInterface
  link?: NetworkLink
}

const MODE_OPTIONS: ReadonlyArray<{ value: ManualNetworkMode; label: string }> = [
  { value: 'dhcp', label: 'DHCP' },
  { value: 'static', label: 'Static' },
  { value: 'link_only', label: 'Link only' },
]

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

  useEffect(() => {
    void load()
  }, [load])

  const openEditor = (iface: NetworkInterface, link?: NetworkLink) => {
    const writableMode: ManualNetworkMode = link?.configurationState === 'static' ? 'static' : link?.configurationState === 'link_only' ? 'link_only' : 'dhcp'
    setMode(writableMode)
    setSubnetId(link?.subnetId || (iface.availableSubnets.length === 1 ? iface.availableSubnets[0].id : ''))
    setIPAddress(link?.configurationState === 'static' ? link.ipAddress : '')
    setDefaultGateway(link?.defaultGateway ?? false)
    setEditor({ iface, link })
  }

  // Non-static modes cannot carry a static address or gateway, so switching away clears them.
  const changeMode = (next: string | null) => {
    if (!next) return
    const value = next as ManualNetworkMode
    setMode(value)
    if (value !== 'static') {
      setIPAddress('')
      setDefaultGateway(false)
    }
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
    return state.target.network.interfaces.flatMap<{ iface: NetworkInterface; link?: NetworkLink }>((iface) =>
      iface.links.length > 0 ? iface.links.map((link) => ({ iface, link })) : [{ iface, link: undefined }],
    )
  }, [state])

  if (state.status === 'loading') return <LoadingState rows={5} />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={() => void load()} />

  return (
    <div className="sw-activity-stack">
      <section className="sw-section">
        <SectionHeader
          title="Network interfaces"
          description="Address configuration and physical link state are reported separately. Changes are available only while the Server is Ready and unlocked."
          actions={
            <Button variant="outline" onClick={() => void load()}>
              Refresh
            </Button>
          }
        />
        {!state.target.editable && (
          <Alert status="info" title="Network configuration is read-only">
            {state.target.disabledReason}
          </Alert>
        )}
        {rows.length === 0 ? (
          <EmptyState title="No network interfaces" message="The provisioner did not report any NICs for this Server." />
        ) : (
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Server network interfaces" className="sw-network-assignment-table">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Interface</Table.ColumnHeader><Table.ColumnHeader>MAC address</Table.ColumnHeader><Table.ColumnHeader>Boot NIC</Table.ColumnHeader><Table.ColumnHeader>Physical link</Table.ColumnHeader><Table.ColumnHeader>Configuration</Table.ColumnHeader><Table.ColumnHeader>Subnet</Table.ColumnHeader><Table.ColumnHeader>IP address</Table.ColumnHeader><Table.ColumnHeader>Gateway</Table.ColumnHeader><Table.ColumnHeader>Provider mode</Table.ColumnHeader><Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map(({ iface, link }, index) => (
                  <Table.Row key={`${iface.id}:${link?.id ?? index}`}>
                    <Table.Cell><strong>{iface.name || '-'}</strong></Table.Cell>
                    <Table.Cell className="sw-mono">{iface.macAddress || '-'}</Table.Cell>
                    <Table.Cell>{iface.boot ? <Badge colorPalette="blue" variant="subtle">Boot</Badge> : '-'}</Table.Cell>
                    <Table.Cell>{iface.physicalState || 'unknown'}</Table.Cell>
                    <Table.Cell>{configurationLabel(link?.configurationState ?? iface.configurationState, link?.rawProviderMode ?? iface.rawProviderMode)}</Table.Cell>
                    <Table.Cell>{link?.subnetName || link?.cidr || '-'}</Table.Cell>
                    <Table.Cell className="sw-mono">{link?.ipAddress || '-'}</Table.Cell>
                    <Table.Cell>{link?.defaultGateway ? 'Default' : '-'}</Table.Cell>
                    <Table.Cell className="sw-mono">{link?.rawProviderMode || iface.rawProviderMode || '-'}</Table.Cell>
                    <Table.Cell textAlign="end">
                      <span className="sw-row-actions">
                        <Button variant="plain" size="sm" px="1" h="auto" colorPalette="brand" disabled={!state.target.editable} onClick={() => openEditor(iface, link)}>
                          {link ? 'Configure' : 'Add link'}
                        </Button>
                        {link && (
                          <Button variant="plain" size="sm" px="1" h="auto" colorPalette="red" disabled={!state.target.editable} onClick={() => setUnbind({ iface, link })}>
                            Unbind
                          </Button>
                        )}
                      </span>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )}
      </section>

      {editor && (
        <Modal
          open
          onClose={() => !saving && setEditor(null)}
          closeOnInteractOutside={!saving}
          title={editor.link ? `Configure ${editor.iface.name}` : `Add link to ${editor.iface.name}`}
          description="Swallow replaces only the selected link and leaves every other NIC and subnet link untouched."
          onSubmit={(event) => {
            event.preventDefault()
            void saveLink()
          }}
          footer={
            <>
              <Button variant="ghost" disabled={saving} onClick={() => setEditor(null)}>
                Cancel
              </Button>
              <Button type="submit" colorPalette="brand" loading={saving} disabled={!validEditor || saving}>
                Save configuration
              </Button>
            </>
          }
        >
          <Stack gap="4">
            <Field.Root required>
              <Field.Label>Mode</Field.Label>
              <SegmentGroup.Root value={mode} onValueChange={(details) => changeMode(details.value)}>
                <SegmentGroup.Indicator />
                {MODE_OPTIONS.map((option) => (
                  <SegmentGroup.Item key={option.value} value={option.value}>
                    <SegmentGroup.ItemText>{option.label}</SegmentGroup.ItemText>
                    <SegmentGroup.ItemHiddenInput />
                  </SegmentGroup.Item>
                ))}
              </SegmentGroup.Root>
            </Field.Root>
            <Field.Root required>
              <Field.Label>
                Subnet <Field.RequiredIndicator />
              </Field.Label>
              <Select
                value={subnetId}
                aria-label="Subnet"
                placeholder="Select a subnet"
                onChange={setSubnetId}
                options={editor.iface.availableSubnets.map((subnet) => ({ value: subnet.id, label: formatSubnetOptionLabel(subnet) }))}
              />
            </Field.Root>
            {mode === 'static' && (
              <Field.Root required invalid={Boolean(ipAddress) && !validStaticIP}>
                <Field.Label>
                  IPv4 address <Field.RequiredIndicator />
                </Field.Label>
                <Input value={ipAddress} onChange={(event) => setIPAddress(event.target.value)} placeholder="192.0.2.10" />
                <Field.HelperText>Enter a valid IPv4 address.</Field.HelperText>
                <Field.ErrorText>Enter a valid IPv4 address.</Field.ErrorText>
              </Field.Root>
            )}
            {mode === 'static' && (
              <Field.Root>
                <Checkbox id="network-default-gateway" checked={defaultGateway} onCheckedChange={setDefaultGateway}>
                  Use this subnet for the default route
                </Checkbox>
                <Field.HelperText>The provider uses the gateway address configured on this subnet for the Server's IPv4 default route.</Field.HelperText>
              </Field.Root>
            )}
          </Stack>
        </Modal>
      )}

      {unbind?.link && (
        <Modal
          open
          onClose={() => !saving && setUnbind(null)}
          closeOnInteractOutside={!saving}
          title="Unbind network link"
          footer={
            <>
              <Button variant="ghost" disabled={saving} onClick={() => setUnbind(null)}>
                Cancel
              </Button>
              <Button colorPalette="red" loading={saving} onClick={() => void unlink()}>
                Unbind
              </Button>
            </>
          }
        >
          <Alert status="warning" title="The interface will lose this configuration">
            {unbind.link.ipAddress || unbind.link.subnetName || 'This subnet link'} will be removed from{' '}
            {unbind.iface.name}; all other links are preserved. Unbind is available only while the Server is Ready
            and unlocked.
          </Alert>
        </Modal>
      )}
    </div>
  )
}

/** Live block-device table. */
export function ServerStorageTab() {
  return <ServerDetailTableTab title="Storage" />
}
/** Live PCI hardware map. */
export function ServerPciTab() {
  return <ServerDetailTableTab title="PCI devices" />
}
