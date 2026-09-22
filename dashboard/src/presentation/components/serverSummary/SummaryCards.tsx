import { Badge, Box, Card, Heading, HStack, IconButton, Link as ChakraLink, Text } from '@chakra-ui/react'
import { Cpu, Eye, EyeOff, HardDrive, MemoryStick, Microchip, Tags } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import type { DetailSection, Server } from '@/domain/server/types'
import { HealthBadge, LockBadge, MembershipBadge, PowerBadge, ProvisioningBadge } from '@/presentation/components/AxisBadge'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { CopyButton } from '@/presentation/components/CopyButton'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { DescriptionList, type DescriptionItem } from '@/presentation/components/ui/description-list'

interface CapacityItem {
  label: string
  value: string
  detail?: string
  icon: ReactNode
}

/**
 * Presents the independent provider, membership, health, and lock axes without collapsing
 * them into one synthetic Server status. Provider lifecycle facts remain secondary rows so
 * an operator can scan the axes before reading the current OS and validation detail.
 */
export function StatusCard({ server, deployedImageHref }: { server: Server; deployedImageHref?: string }) {
  const axis = server.provisioning
  return (
    <Card.Root className="sw-server-summary-card">
      <Card.Body gap="4">
        <Box>
          <Heading size="sm">Current state</Heading>
          <Text color="fg.muted" fontSize="sm" mt="1">
            Independent operational signals from Swallow and the provisioner.
          </Text>
        </Box>
        <HStack gap="2" wrap="wrap">
          <ProvisioningBadge axis={axis} />
          <LockBadge locked={axis?.locked ?? false} />
          <MembershipBadge axis={server.membership} />
          <HealthBadge axis={server.health} />
        </HStack>
        {axis && (
          <DescriptionList
            items={[
              {
                label: 'Power',
                value: (
                  <HStack gap="2">
                    <PowerBadge powerState={axis.powerState} decorative />
                    <Text>{powerStateLabel(axis.powerState)}</Text>
                  </HStack>
                ),
              },
              { label: 'Provider state', value: axis.providerState },
              // The machine-level failure reason (e.g. "Failed to erase disks.") is shown only when
              // the provider reported one, so a healthy Server does not carry an empty "Reason" row.
              ...(axis.errorDescription?.trim() ? [{ label: 'Provider reason', value: axis.errorDescription.trim() }] : []),
              {
                label: 'Deployed OS',
                value: axis.deployedImageName
                  ? deployedImageHref
                    ? (
                        <ChakraLink
                          asChild
                          color="brand.fg"
                          fontWeight="medium"
                          textDecoration="underline"
                          textUnderlineOffset="3px"
                        >
                          <RouterLink to={deployedImageHref}>{axis.deployedImageName}</RouterLink>
                        </ChakraLink>
                      )
                    : axis.deployedImageName
                  : null,
              },
              {
                label: 'Ephemeral',
                value: axis.state === 'deployed' ? (axis.ephemeral ? 'Yes — disk changes are lost on reboot' : 'No') : null,
              },
              { label: 'Kernel', value: axis.hweKernel },
              { label: 'Commissioning', value: axis.commissioningStatus },
              { label: 'Testing', value: axis.testingStatus },
            ]}
          />
        )}
      </Card.Body>
    </Card.Root>
  )
}

/**
 * Consolidates capacity into one scan surface rather than four equally weighted cards.
 * Values come from the reconciled Server projection; missing observations are explicit and
 * GPU count is derived from every reported accelerator model.
 */
export function CapacityCard({ server, storageDeviceCount }: { server: Server; storageDeviceCount?: number }) {
  const gpuCount = server.gpus.reduce((total, gpu) => total + gpu.count, 0)
  const gpuDetail = server.gpus.map((gpu) => `${gpu.vendor} ${gpu.model}`).join(', ')
  const items: CapacityItem[] = [
    {
      label: 'CPU',
      value: server.cpuCores ? `${server.cpuCores} cores` : 'Not observed',
      detail: [server.cpuModel, server.architecture].filter(Boolean).join(' · ') || undefined,
      icon: <Cpu size={18} />,
    },
    {
      label: 'Memory',
      value: server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : 'Not observed',
      icon: <MemoryStick size={18} />,
    },
    {
      label: 'Storage',
      value: server.storageGB ? `${Math.round(server.storageGB)} GB` : 'Not observed',
      detail: storageDeviceCount ? `${storageDeviceCount} reported device${storageDeviceCount === 1 ? '' : 's'}` : undefined,
      icon: <HardDrive size={18} />,
    },
    {
      label: 'Accelerators',
      value: gpuCount ? `${gpuCount} GPU${gpuCount === 1 ? '' : 's'}` : 'None reported',
      detail: gpuDetail || undefined,
      icon: <Microchip size={18} />,
    },
  ]
  return (
    <Card.Root className="sw-server-summary-card">
      <Card.Body gap="4">
        <Box>
          <Heading size="sm">Capacity</Heading>
          <Text color="fg.muted" fontSize="sm" mt="1">
            Reconciled compute, memory, storage, and accelerator inventory.
          </Text>
        </Box>
        <Box as="dl" className="sw-server-capacity-grid">
          {items.map((item) => (
            <Box as="div" key={item.label} className="sw-server-capacity-item">
              <HStack as="dt" gap="2">
                <Box aria-hidden className="sw-server-capacity-icon">{item.icon}</Box>
                <Text color="fg.muted" fontSize="sm" fontWeight="medium">{item.label}</Text>
              </HStack>
              <Text as="dd" className="sw-server-capacity-value">{item.value}</Text>
              {item.detail && <Text className="sw-server-capacity-detail">{item.detail}</Text>}
            </Box>
          ))}
        </Box>
      </Card.Body>
    </Card.Root>
  )
}

/**
 * Shows provider placement and labels, with an icon-only tag editor kept next to the tags it
 * changes. The Tooltip and accessible name make the compact control understandable without
 * relying on the glyph.
 */
export function DetailsCard({ server, onEditTags }: { server: Server; onEditTags?: () => void }) {
  return (
    <Card.Root className="sw-server-summary-card">
      <Card.Body gap="4">
        <HStack justify="space-between" gap="2">
          <Box>
            <Heading size="sm">Placement and labels</Heading>
            <Text color="fg.muted" fontSize="sm" mt="1">Provider grouping and operational tags.</Text>
          </Box>
          {onEditTags && (
            <Tooltip content="Edit tags">
              <IconButton size="sm" variant="ghost" aria-label="Edit tags" onClick={onEditTags}>
                <Tags size={18} />
              </IconButton>
            </Tooltip>
          )}
        </HStack>
        <DescriptionList
          items={[
            { label: 'Zone', value: server.providerZone },
            { label: 'Resource pool', value: server.providerResourcePool },
            { label: 'VM host', value: server.providerPod },
            { label: 'Tags', value: server.tags.length ? <TagBadges tags={server.tags} /> : null },
          ]}
        />
      </Card.Body>
    </Card.Root>
  )
}

const COPYABLE_MANAGEMENT_FIELDS = new Set(['Address', 'Username', 'Node ID', 'Power MAC'])
const MASKED_PASSWORD = '••••••••'

/** Keeps the BMC password transient and masked until an operator explicitly reveals it. */
function ManagementPassword({ value }: { value: string }) {
  const [visible, setVisible] = useState(false)
  const visibilityLabel = visible ? 'Hide password' : 'Show password'
  return (
    <HStack gap="1">
      <Text
        as="span"
        className="sw-mono"
        aria-label={visible ? value : 'Password hidden'}
        aria-live="polite"
      >
        {visible ? value : MASKED_PASSWORD}
      </Text>
      <Tooltip content={visibilityLabel}>
        <IconButton
          size="2xs"
          variant="ghost"
          color="fg.muted"
          aria-label={visibilityLabel}
          onClick={() => setVisible((current) => !current)}
        >
          {visible ? <EyeOff size={14} /> : <Eye size={14} />}
        </IconButton>
      </Tooltip>
      <CopyButton value={value} label="Copy password" />
    </HStack>
  )
}

/**
 * Promotes out-of-band access to its own operator surface. Connection identities are copyable;
 * the explicitly allowlisted password stays masked until an operator asks to reveal it.
 */
export function ManagementControllerCard({ management }: { management?: DetailSection }) {
  const items: DescriptionItem[] =
    management?.fields.map((field) => ({
      label: field.label,
      value: field.label === 'Password' ? (
        <ManagementPassword value={field.value} />
      ) : COPYABLE_MANAGEMENT_FIELDS.has(field.label) ? (
        <HStack gap="1">
          <Text as="span" className="sw-mono">{field.value}</Text>
          <CopyButton value={field.value} label={`Copy ${field.label.toLocaleLowerCase()}`} />
        </HStack>
      ) : field.value,
    })) ?? []

  return (
    <Card.Root className="sw-server-summary-card">
      <Card.Body gap="4">
        <Box>
          <Heading size="sm">Management controller</Heading>
          <Text color="fg.muted" fontSize="sm" mt="1">
            Out-of-band connection and power configuration for manual administration.
          </Text>
        </Box>
        {items.length > 0 ? (
          <DescriptionList items={items} />
        ) : (
          <Text color="fg.muted" fontSize="sm">
            No IPMI or Redfish controller details were reported by the provisioner.
          </Text>
        )}
      </Card.Body>
    </Card.Root>
  )
}

/**
 * Combines reconciled hardware facts with live provider hardware fields. Provider labels win
 * when they overlap so the card remains a compact profile instead of repeating facts.
 */
export function HardwareProfileCard({ server, system }: { server: Server; system?: DetailSection }) {
  const items: DescriptionItem[] = system ? system.fields.map((field) => ({ label: field.label, value: field.value })) : []
  const labels = new Set(items.map((item) => item.label.toLocaleLowerCase()))
  // Provider labels are not standardized, so aliases prevent duplicated projection fallbacks.
  const addFallback = (label: string, value: ReactNode, aliases: readonly string[] = []) => {
    const candidates = [label, ...aliases].map((candidate) => candidate.toLocaleLowerCase())
    if (candidates.some((candidate) => labels.has(candidate))) return
    items.push({ label, value })
    labels.add(label.toLocaleLowerCase())
  }
  addFallback('System vendor', server.systemVendor)
  addFallback('Serial number', server.hardware.serialNumber, ['Serial'])
  addFallback('System product', server.systemProduct)
  addFallback('Architecture', server.architecture)
  addFallback('CPU model', server.cpuModel)
  addFallback('Accelerators', server.gpus.length ? server.gpus.map((gpu) => `${gpu.count} × ${gpu.vendor} ${gpu.model}`).join(', ') : null)
  return (
    <Card.Root className="sw-server-summary-card">
      <Card.Body gap="4">
        <Box>
          <Heading size="sm">Hardware profile</Heading>
          <Text color="fg.muted" fontSize="sm" mt="1">Reconciled hardware identity and architecture.</Text>
        </Box>
        <DescriptionList items={items} />
      </Card.Body>
    </Card.Root>
  )
}

/** Renders operational tags as wrapping text badges; colour only supplies secondary grouping. */
function TagBadges({ tags }: { tags: string[] }) {
  return (
    <HStack gap="1" wrap="wrap">
      {tags.map((tag) => (
        <Badge key={tag} colorPalette="blue" variant="subtle">
          {tag}
        </Badge>
      ))}
    </HStack>
  )
}
