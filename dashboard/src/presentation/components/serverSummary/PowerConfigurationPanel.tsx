import { Box, Heading, HStack, Stack, Text } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import type { ServerPowerConfiguration } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList, type DescriptionItem } from '@/presentation/components/ui/description-list'
import {
  powerControlDetail,
  powerControlLabel,
  powerControlStatus,
  powerDriverLabel,
  powerFamilyLabel,
} from './powerConfigurationLabels'

/** The panel's read state, mirroring useAsyncData without importing the hook. */
export type PowerConfigurationPanelState =
  | { status: 'loading' }
  | { status: 'unavailable' | 'error'; message: string }
  | { status: 'ready'; data: ServerPowerConfiguration }

interface PowerConfigurationPanelProps {
  state: PowerConfigurationPanelState
  /**
   * Leave out the connection parameters (address, account, password) because the caller shows them
   * already — the Management controller card's BMC facts, read from the provisioner detail.
   */
  compact?: boolean
  /** The caller's controls (edit), rendered under the facts. */
  actions?: ReactNode
}

/**
 * One Server's Power Configuration (glossary Power Configuration, decision 054), shown inside the
 * Management controller card on the Server Summary: the provisioner's power driver and its family,
 * what that driver lets the provisioner do (control), and the driver's parameters.
 *
 * The data is the provisioner's live configuration, read through the API; swallow stores none of
 * it. The password is write-only, so only whether one is set is shown. A Server with no power
 * control gets a warning, because the provisioner then cannot inspect or deploy it and its hardware
 * inspection stops for attention; a virtual machine powered through a provisioner VM host shows why
 * it is read-only. Loading and failures are text, never an empty block, and every state is spelled
 * out in words; badges only add colour. The page injects the actions so this block stays
 * page-agnostic.
 */
export function PowerConfigurationPanel({ state, compact = false, actions }: PowerConfigurationPanelProps) {
  return (
    <Stack gap="3" aria-labelledby="server-power-configuration-title" as="section">
      <Box>
        {/* h3: a sub-section of the card's own h2 heading, so the outline stays nested. */}
        <Heading as="h3" size="xs" id="server-power-configuration-title">Power configuration</Heading>
        <Text color="fg.muted" fontSize="sm" mt="1">
          How the provisioner switches and reads this Server&apos;s power. Saved to the provisioner; swallow keeps no copy.
        </Text>
      </Box>
      <PowerConfigurationFacts state={state} compact={compact} />
      {actions && (
        <HStack gap="2" wrap="wrap">
          {actions}
        </HStack>
      )}
    </Stack>
  )
}

/** A monospace value with a copy control, for identities an operator pastes elsewhere. */
function CopyableValue({ value, label }: { value: string; label: string }) {
  return (
    <HStack gap="1" minW="0">
      <Text as="span" className="sw-mono" wordBreak="break-all">{value}</Text>
      <CopyButton value={value} label={label} />
    </HStack>
  )
}

/** The facts for one read state. */
function PowerConfigurationFacts({ state, compact }: { state: PowerConfigurationPanelState; compact: boolean }) {
  if (state.status === 'loading') {
    return <Text color="fg.muted" fontSize="sm">Loading power configuration…</Text>
  }
  if (state.status !== 'ready') {
    return (
      <Alert status="warning" title="Power configuration unavailable">
        {state.message}
      </Alert>
    )
  }
  const config = state.data
  const family = powerFamilyLabel(config.family)
  const items: DescriptionItem[] = [
    {
      label: 'Driver',
      value: family ? `${powerDriverLabel(config.driver)} — ${family}` : powerDriverLabel(config.driver),
    },
    {
      label: 'Control',
      value: (
        <Stack gap="1">
          <Box>
            <StatusBadge status={powerControlStatus(config.control)} label={powerControlLabel(config.control)} />
          </Box>
          <Text as="span" color="fg.muted" fontSize="sm">{powerControlDetail(config.control)}</Text>
        </Stack>
      ),
    },
  ]
  if (!compact || config.family !== 'bmc') {
    if (config.address) items.push({ label: 'Address', value: <CopyableValue value={config.address} label="Copy power address" /> })
    if (config.powerId) items.push({ label: 'Power ID', value: <CopyableValue value={config.powerId} label="Copy power ID" /> })
    if (config.username) items.push({ label: 'Account', value: <CopyableValue value={config.username} label="Copy power account" /> })
    if (config.driver) items.push({ label: 'Password', value: config.passwordSet ? 'Set (write-only)' : 'Not set' })
  }

  return (
    <Stack gap="3">
      <DescriptionList items={items} />
      {config.control === 'none' && (
        <Alert status="warning" title="No power control">
          Set a power configuration so the provisioner can power this Server. For a libvirt virtual machine, choose virsh with
          its hypervisor&apos;s qemu+ssh URI and the domain name; the provisioner&apos;s rack controller needs SSH access to that
          hypervisor.
        </Alert>
      )}
      {!config.editable && config.readOnlyReason && (
        <Alert status="info" title="Managed by the provisioner">
          {config.readOnlyReason}
        </Alert>
      )}
    </Stack>
  )
}
