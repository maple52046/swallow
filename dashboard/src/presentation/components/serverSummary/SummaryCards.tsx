import { Badge, Box, Card, Heading, HStack, IconButton, Link as ChakraLink, Text } from '@chakra-ui/react'
import { Cpu, Eye, EyeOff, HardDrive, MemoryStick, Microchip, Tags } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import { serverPrimaryAddress, type DetailSection, type Server } from '@/domain/server/types'
import { HealthBadge, LockBadge, MembershipBadge, PowerBadge, ProvisioningBadge } from '@/presentation/components/AxisBadge'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { CopyButton } from '@/presentation/components/CopyButton'
import { defaultUserSourceLabel } from '@/presentation/components/serverSummary/defaultUserLabels'
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
 * `bootMedia` is the caller's Boot Media block (BootMediaPanel), rendered under the connection
 * facts because it is driven through this same controller; the card stays page-agnostic.
 */
export function ManagementControllerCard({ management, bootMedia }: { management?: DetailSection; bootMedia?: ReactNode }) {
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
    // A named region, like Connection, so assistive technology can jump to out-of-band access.
    <Card.Root as="section" className="sw-server-summary-card" aria-labelledby="server-management-title">
      <Card.Body gap="4">
        <Box>
          <Heading size="sm" id="server-management-title">Management controller</Heading>
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
        {bootMedia}
      </Card.Body>
    </Card.Root>
  )
}

/**
 * In-band SSH access to a deployed Server, the counterpart of the out-of-band management
 * controller card. It shows the login user — the effective Server Default User the API resolves
 * (set on this Server, else the deployed OS image's default user; decisions 045 and 039) with
 * where it comes from — the primary address, and the `ssh <user>@<address>` command an operator
 * runs with one of their Access Keys.
 *
 * A command is offered only when every part is known; otherwise the missing fact is spelled
 * out instead of producing a half-formed command: nothing deployed yet, no reported address, or
 * no known default user (automation then probes fallback users, so the login cannot be
 * predicted). `imageHref` deep-links to the deployed OS image, where the image's default user is
 * set. `loginUserAction` is the caller's control for setting the Server's own default user,
 * rendered beside the login user so the card stays a shared, page-agnostic component.
 */
export function ConnectionCard({ server, imageHref, loginUserAction }: { server: Server; imageHref?: string; loginUserAction?: ReactNode }) {
  const axis = server.provisioning
  const deployed = axis?.state === 'deployed'
  const user = server.defaultUser?.user
  const address = serverPrimaryAddress(server)
  const command = deployed && user && address ? `ssh ${user}@${address}` : ''
  const unknownUser = imageHref ? (
    <Text as="span" color="fg.muted">
      Unknown — set one here, or{' '}
      <ChakraLink asChild color="brand.fg" textDecoration="underline" textUnderlineOffset="3px">
        <RouterLink to={imageHref}>set the OS image&apos;s default user</RouterLink>
      </ChakraLink>
    </Text>
  ) : (
    <Text as="span" color="fg.muted">Unknown — set the default user</Text>
  )
  const loginUser = (
    <HStack gap="2" wrap="wrap">
      {user ? (
        <>
          <Text as="span" className="sw-mono">{user}</Text>
          <Text as="span" color="fg.muted" fontSize="sm">
            ({defaultUserSourceLabel(server.defaultUser?.source ?? 'os_image')})
          </Text>
        </>
      ) : (
        unknownUser
      )}
      {loginUserAction}
    </HStack>
  )
  const items: DescriptionItem[] = [
    { label: 'Login user', value: loginUser },
    {
      label: 'Address',
      value: address ? (
        <HStack gap="1">
          <Text as="span" className="sw-mono">{address}</Text>
          <CopyButton value={address} label="Copy address" />
        </HStack>
      ) : (
        <Text as="span" color="fg.muted">Not reported by the provisioner yet</Text>
      ),
    },
  ]

  return (
    // A named region, so assistive technology can jump straight to how to reach the Server.
    <Card.Root as="section" className="sw-server-summary-card" aria-labelledby="server-connection-title">
      <Card.Body gap="4">
        <Box>
          <Heading size="sm" id="server-connection-title">Connection</Heading>
          <Text color="fg.muted" fontSize="sm" mt="1">
            In-band SSH access to the deployed operating system.
          </Text>
        </Box>
        {deployed ? (
          <>
            <DescriptionList items={items} />
            {command && (
              // The command is one unbroken line (it scrolls rather than wraps) so it reads and
              // copies exactly as it is typed into a terminal.
              <HStack gap="2" bg="bg.muted" rounded="md" ps="3" pe="1" py="1" justify="space-between">
                <Text as="code" className="sw-mono" fontSize="sm" whiteSpace="nowrap" overflowX="auto">
                  {command}
                </Text>
                <CopyButton value={command} label="Copy SSH command" />
              </HStack>
            )}
            <Text color="fg.muted" fontSize="sm">
              Authenticate with one of your access keys (account menu → SSH keys).
            </Text>
          </>
        ) : (
          <Text color="fg.muted" fontSize="sm">
            SSH access is available after an operating system is deployed.
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
