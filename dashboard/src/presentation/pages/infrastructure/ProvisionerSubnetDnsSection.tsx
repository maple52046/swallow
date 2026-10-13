import { useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ProvisionerSubnet } from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { EmptyState } from '@/presentation/components/EmptyState'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
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
 * The values are provisioner-owned and live: read from MAAS when the tab opens and written
 * through by each subnet's own **Apply DNS** button, never by the General or Connection Save. Each subnet also
 * says in words whether MAAS runs its DHCP, because that decides whether the setting matters: on a
 * site-DHCP subnet DHCP deployments take DNS from the site server instead.
 */
export function ProvisionerSubnetDnsSection({ integration }: ProvisionerSubnetDnsSectionProps) {
  return (
    <ProvisionerLiveSettingSection
      integration={integration}
      id="integration-subnet-dns"
      title="Subnet DNS"
      description="The DNS servers MAAS gives machines deployed with an Automatic or Static address on each subnet; DHCP deployments use the DHCP server's. They are read from and applied to MAAS directly, not saved with this Integration."
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
        const primary = list.filter((subnet) => subnet.attached || subnet.dnsServers.length > 0)
        const other = list.filter((subnet) => !subnet.attached && subnet.dnsServers.length === 0)
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
 * One subnet's editable DNS servers. `applied` is what MAAS last reported (the read, then each
 * read-back), so "Not applied" compares the operator's text with MAAS rather than with the opening
 * state; text that reads as the same list (other separators or spacing) is not a change.
 */
function SubnetDnsRow({ integrationId, subnet }: { integrationId: string; subnet: ProvisionerSubnet }) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [applied, setApplied] = useState(subnet)
  const [text, setText] = useState(formatDnsServerList(subnet.dnsServers))
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState('')

  const parsed = parseDnsServerList(text)
  const dirty = parsed.servers === null || formatDnsServerList(parsed.servers) !== formatDnsServerList(applied.dnsServers)
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
      setApplied(result)
      setText(formatDnsServerList(result.dnsServers))
      showToast({
        tone: 'success',
        title: `DNS applied to ${result.cidr}`,
        description: 'MAAS uses it for Automatic and Static deployments it starts from now on.',
      })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The DNS servers could not be applied.')
    } finally {
      setApplying(false)
    }
  }

  const appliedSummary = applied.dnsServers.length > 0 ? formatDnsServerList(applied.dnsServers) : 'its own DNS (none set)'

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
          <Alert status="error" title="DNS servers could not be applied">
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
              // Enter applies this subnet's DNS, which is what an operator typing an address means;
              // the default is suppressed so no enclosing form ever submits instead.
              if (event.key !== 'Enter') return
              event.preventDefault()
              void apply()
            }}
          />
          {parsed.problem !== null && <Field.ErrorText>{parsed.problem}</Field.ErrorText>}
        </Field.Root>
        <HStack justify="space-between" gap="3" wrap="wrap">
          <Text role="status" fontSize="sm" color={dirty ? 'fg' : 'fg.muted'}>
            {dirty ? 'Not applied yet.' : `MAAS uses: ${appliedSummary}.`}
          </Text>
          <Button type="button" size="sm" colorPalette="brand" variant="outline" loading={applying} disabled={!canApply} onClick={() => void apply()}>
            Apply DNS
          </Button>
        </HStack>
      </Stack>
    </li>
  )
}
