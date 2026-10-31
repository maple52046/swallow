import { useCallback, useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ProvisionerSubnet } from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConvergenceStatus } from '@/presentation/components/convergence/ConvergenceStatus'
import { describeConvergence, isSettling } from '@/presentation/components/convergence/convergencePresentation'
import { EmptyState } from '@/presentation/components/EmptyState'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'
import { ProvisionerLiveSettingSection } from './ProvisionerLiveSettingSection'
import { formatDnsServerList, parseDnsServerList } from './subnetDnsForm'

interface ProvisionerSubnetDnsSectionProps {
  /** The stored MAAS provisioner Integration whose settings page shows this tab. */
  integration: Pick<Integration, 'id' | 'hasCredential'>
}

/**
 * The Subnet DNS tab of a MAAS provisioner's Integration settings page (contract
 * provisioner-subnet-dns.md, decision 058): per subnet, the DNS servers MAAS gives machines
 * deployed there with an Automatic or Static address.
 *
 * The subnets are MAAS's and are read live when the tab opens; each subnet's DNS servers are
 * desired state swallow keeps (decision 071), saved by the subnet's own **Save DNS** button, never by
 * the General or Connection Save, and applied to MAAS by the backend. Each subnet also says in words
 * whether MAAS runs its DHCP, because that decides whether the setting matters: on a site-DHCP
 * subnet DHCP deployments take DNS from the site server instead.
 */
export function ProvisionerSubnetDnsSection({ integration }: ProvisionerSubnetDnsSectionProps) {
  return (
    <ProvisionerLiveSettingSection
      integration={integration}
      id="integration-subnet-dns"
      title="Subnet DNS"
      description="The DNS servers MAAS gives machines deployed with an Automatic or Static address on each subnet; DHCP deployments use the DHCP server's. swallow keeps them and applies them to MAAS; they are not saved with this Integration's connection."
      credentialNeededFor="reads and applies subnet DNS"
    >
      <LiveSubnets integrationId={integration.id} />
    </ProvisionerLiveSettingSection>
  )
}

/**
 * Reads the subnet catalog once per mount (re-read when the Integration changes). A 404 means the
 * Integration was deleted meanwhile and renders as unavailable, not as a fault; a provisioner
 * without subnets renders the shared empty state.
 *
 * Attached subnets, and any subnet that already has DNS servers (so a configured setting is never
 * tucked away), are listed first; the rest — host-internal networks MAAS recorded from enrolled
 * hosts, which can be one per host on a container or Kubernetes fleet — sit in a collapsed
 * disclosure that still lets an operator edit them.
 */
function LiveSubnets({ integrationId }: { integrationId: string }) {
  const { provisioning } = useApp()
  const subnets = useAsyncData(() => provisioning.listProvisionerSubnets(integrationId), [provisioning, integrationId])
  return (
    <AsyncSection
      state={subnets}
      loadingRows={3}
      unavailableTitle="Subnet DNS settings are unavailable"
      unavailableHint="Return to the Integrations list and refresh it."
    >
      {(list) => {
        if (list.length === 0) {
          return <EmptyState title="No subnets" message="MAAS reports no subnets for this provisioner yet." />
        }
        const configured = (subnet: ProvisionerSubnet) => subnet.spec.dnsServers.length > 0 || subnet.status.dnsServers.length > 0
        const primary = list.filter((subnet) => subnet.attached || configured(subnet))
        const other = list.filter((subnet) => !subnet.attached && !configured(subnet))
        return (
          <Stack gap="4">
            {primary.length > 0 ? (
              <ul className="sw-subnet-dns-list" aria-label="Subnets">
                {primary.map((subnet) => (
                  <SubnetDnsRow key={subnet.id} integrationId={integrationId} subnet={subnet} />
                ))}
              </ul>
            ) : (
              <Text fontSize="sm" color="fg.muted">
                No MAAS controller, MAAS DHCP, boot interface, or hardware NIC is on any subnet yet.
              </Text>
            )}
            {other.length > 0 && (
              <details className="sw-subnet-dns-other">
                <summary>Other subnets ({other.length})</summary>
                <Text fontSize="sm" color="fg.muted">
                  Networks MAAS recorded from the hosts it knows — container, Kubernetes, or VM bridges, or
                  networks nothing is on any more. No MAAS controller, MAAS DHCP, boot interface, or hardware NIC
                  is on them, so deployments do not use them.
                </Text>
                <ul className="sw-subnet-dns-list" aria-label="Other subnets">
                  {other.map((subnet) => (
                    <SubnetDnsRow key={subnet.id} integrationId={integrationId} subnet={subnet} />
                  ))}
                </ul>
              </details>
            )}
          </Stack>
        )
      }}
    </AsyncSection>
  )
}

/**
 * One subnet's editable DNS servers. `saved` is the subnet as last read or saved, so "Not saved yet"
 * compares the operator's text with the spec rather than with the opening state; text that reads as
 * the same list (other separators or spacing) is not a change. The status line says whether MAAS
 * holds the spec and what MAAS reports; while a saved change is still being applied, the subnet list
 * is re-read quietly and this row takes its subnet from it until it settles.
 */
function SubnetDnsRow({ integrationId, subnet }: { integrationId: string; subnet: ProvisionerSubnet }) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [saved, setSaved] = useState(subnet)
  const [text, setText] = useState(formatDnsServerList(subnet.spec.dnsServers))
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState('')

  const settling = isSettling(describeConvergence(saved.status.conditions, saved.generation).state)
  const refresh = useCallback(async () => {
    const latest = (await provisioning.listProvisionerSubnets(integrationId)).find((item) => item.id === subnet.id)
    if (latest) setSaved(latest)
  }, [provisioning, integrationId, subnet.id])
  useSettlingRefresh(settling, refresh)

  const parsed = parseDnsServerList(text)
  const dirty = parsed.servers === null || formatDnsServerList(parsed.servers) !== formatDnsServerList(saved.spec.dnsServers)
  const canApply = parsed.servers !== null && dirty && !applying
  const inputId = `subnet-dns-${subnet.id}`
  // MAAS names a subnet after its CIDR unless an operator renamed it; only a real name adds a line.
  const name = subnet.name && subnet.name !== subnet.cidr ? subnet.name : ''

  const apply = async () => {
    if (!canApply || parsed.servers === null) return
    setApplying(true)
    setError('')
    try {
      const result = await provisioning.setProvisionerSubnetDnsServers(integrationId, subnet.id, parsed.servers)
      setSaved(result)
      setText(formatDnsServerList(result.spec.dnsServers))
      showToast({
        tone: 'success',
        title: `DNS saved for ${result.cidr}`,
        description: 'swallow applies it to MAAS shortly; Automatic and Static deployments MAAS starts afterwards use it.',
      })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The DNS servers could not be saved.')
    } finally {
      setApplying(false)
    }
  }

  const observedSummary = saved.status.dnsServers.length > 0 ? formatDnsServerList(saved.status.dnsServers) : 'its own DNS (none set)'

  return (
    <li className="sw-subnet-dns-row">
      <Stack gap="0.5" className="sw-subnet-dns-row__identity">
        <Text fontWeight="semibold" className="sw-mono">
          {subnet.cidr}
        </Text>
        {name && <Text fontSize="sm">{name}</Text>}
        <Text fontSize="sm" color="fg.muted">
          {subnet.provisionerDhcp ? 'DHCP: MAAS' : 'DHCP: not MAAS (site DHCP, if any)'}
          {subnet.gatewayAddress ? ` · Gateway ${subnet.gatewayAddress}` : ''}
        </Text>
      </Stack>
      <Stack gap="2" className="sw-subnet-dns-row__editor">
        {error && (
          <Alert status="error" title="DNS servers could not be saved">
            {error}
          </Alert>
        )}
        <Field.Root invalid={parsed.problem !== null}>
          <Field.Label htmlFor={inputId}>DNS servers for {subnet.cidr}</Field.Label>
          {/* The rule sits above the field so an error below it never hides the instructions; as a
              helper text it is still announced with the input. */}
          <Field.HelperText mt="0" mb="1">
            IP addresses in order, separated by commas or spaces; at most 8. Empty uses the MAAS DNS.
          </Field.HelperText>
          <Input
            id={inputId}
            value={text}
            placeholder="MAAS DNS"
            disabled={applying}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              // Enter saves this subnet's DNS, which is what an operator typing an address means;
              // the default is suppressed so no enclosing form ever submits instead.
              if (event.key !== 'Enter') return
              event.preventDefault()
              void apply()
            }}
          />
          {parsed.problem !== null && <Field.ErrorText>{parsed.problem}</Field.ErrorText>}
        </Field.Root>
        <HStack justify="space-between" align="flex-start" gap="3" wrap="wrap">
          <Stack gap="1" role="status">
            {dirty ? (
              <Text fontSize="sm">Not saved yet.</Text>
            ) : (
              <ConvergenceStatus conditions={saved.status.conditions} generation={saved.generation} />
            )}
            <Text fontSize="sm" color="fg.muted">
              MAAS reports: {observedSummary}.
            </Text>
          </Stack>
          <Button type="button" size="sm" colorPalette="brand" variant="outline" loading={applying} disabled={!canApply} onClick={() => void apply()}>
            Save DNS
          </Button>
        </HStack>
      </Stack>
    </li>
  )
}
