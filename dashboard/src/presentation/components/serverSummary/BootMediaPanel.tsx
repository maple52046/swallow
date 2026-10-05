import { Box, Heading, HStack, Stack, Text } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import type { BootMediaLiveState, ServerBootMedia } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList, type DescriptionItem } from '@/presentation/components/ui/description-list'
import { formatRelative } from '@/shared/utils/time'
import { BootMediaApplyProgress } from './BootMediaApplyProgress'
import { bootMediaApplierLabel, bootOverrideLabel, redfishSupportLabel, redfishSupportStatus } from './bootMediaLabels'

/** The panel's read state, mirroring useAsyncData without importing the hook. */
export type BootMediaPanelState =
  | { status: 'loading' }
  | { status: 'unavailable' | 'error'; message: string }
  | { status: 'ready'; data: ServerBootMedia }

interface BootMediaPanelProps {
  state: BootMediaPanelState
  /** A live BMC read the caller requested, shown until the next read; absent when not read. */
  live?: { state: BootMediaLiveState | null; error?: string }
  /** The caller's controls (enable/disable, re-detect, check BMC), rendered under the facts. */
  actions?: ReactNode
}

/**
 * Boot Media for one physical Server (glossary Boot Media and BMC, decision 047), shown inside the
 * Management controller card on the Server Summary.
 *
 * It presents three swallow-owned or swallow-observed facts without inventing a combined status:
 * the BMC's Redfish capability (from swallow's probe, with its age), the Server's Boot Media
 * setting (enabled or not, how persistently the BMC took it, and the last apply or failure), and
 * the chosen Boot ISO (decision 049: built per provisioner on the Boot ISOs tab, with the URL the
 * BMC mounts). An enabled setting that names no Boot ISO (enabled before Boot ISOs existed) or one
 * that cannot be mounted gets a warning, because its OS deployments stop at the Boot Media step.
 * While an enable preflight runs (`apply`, kept fresh by the caller's polling) its progress is
 * shown under the facts, so it stays visible after the dialog is closed or the page reloaded.
 * A live BMC read is shown only when the caller asked for one, because it takes seconds. Every
 * state is spelled out in text; badges only add colour. The page injects the actions so this block
 * stays page-agnostic.
 */
export function BootMediaPanel({ state, live, actions }: BootMediaPanelProps) {
  return (
    <Stack gap="3" borderTopWidth="1px" borderColor="border.muted" pt="4" aria-labelledby="server-boot-media-title" as="section">
      <Box>
        {/* h3: a sub-section of the card's own h2 heading, so the outline stays nested. */}
        <Heading as="h3" size="xs" id="server-boot-media-title">Boot media</Heading>
        <Text color="fg.muted" fontSize="sm" mt="1">
          Boots an iPXE Boot ISO first through the BMC, so the Server reaches the provisioner on a network whose DHCP the
          provisioner does not run.
        </Text>
      </Box>
      <BootMediaFacts state={state} live={live} />
      {actions && (
        <HStack gap="2" wrap="wrap">
          {actions}
        </HStack>
      )}
    </Stack>
  )
}

/** The facts for one read state; loading and failures are text, never an empty block. */
function BootMediaFacts({ state, live }: Pick<BootMediaPanelProps, 'state' | 'live'>) {
  if (state.status === 'loading') {
    return <Text color="fg.muted" fontSize="sm">Loading Boot Media…</Text>
  }
  if (state.status !== 'ready') {
    return (
      <Alert status="warning" title="Boot Media unavailable">
        {state.message}
      </Alert>
    )
  }
  const { image, setting, redfish, apply } = state.data
  const items: DescriptionItem[] = [
    {
      label: 'Redfish',
      value: redfish ? (
        <Stack gap="1">
          <HStack gap="2" wrap="wrap">
            <StatusBadge status={redfishSupportStatus(redfish.support)} label={redfishSupportLabel(redfish.support)} />
            <Text as="span" color="fg.muted" fontSize="sm">
              {[redfish.vendor, redfish.firmwareVersion && `firmware ${redfish.firmwareVersion}`].filter(Boolean).join(' · ')}
              {` · probed ${formatRelative(redfish.probedAt)}`}
            </Text>
          </HStack>
          {redfish.reason && <Text fontSize="sm">{redfish.reason}</Text>}
        </Stack>
      ) : (
        <Text as="span" color="fg.muted">Not probed yet — swallow probes new Servers within minutes.</Text>
      ),
    },
    {
      label: 'Status',
      // A running preflight decides the setting, so it is the status until it ends; the saved
      // setting would read as already settled (for example "Disabled" while being enabled).
      value: apply ? (
        <HStack gap="2" wrap="wrap">
          <StatusBadge status="running" label="Applying" />
          <Text as="span" fontSize="sm">The setting is saved once the BMC confirms the Boot ISO.</Text>
        </HStack>
      ) : setting?.enabled ? (
        <Stack gap="1">
          <HStack gap="2" wrap="wrap">
            <StatusBadge status="active" label="Enabled" />
            {setting.bootOverride && (
              <Text as="span" fontSize="sm">Boots the ISO first on {bootOverrideLabel(setting.bootOverride)}</Text>
            )}
          </HStack>
          {setting.lastAppliedAt && (
            <Text color="fg.muted" fontSize="sm">
              Last applied {formatRelative(setting.lastAppliedAt)} {bootMediaApplierLabel(setting.lastAppliedBy)}
            </Text>
          )}
        </Stack>
      ) : (
        <HStack gap="2">
          <StatusBadge status="unknown" label="Disabled" />
          <Text as="span" color="fg.muted" fontSize="sm">OS deployments do not touch the BMC.</Text>
        </HStack>
      ),
    },
    { label: 'Boot ISO', value: <BootISOFact image={image} /> },
  ]
  if (live) {
    items.push({ label: 'BMC now', value: <LiveState live={live} /> })
  }
  const enabled = setting?.enabled ?? false
  return (
    <Stack gap="3">
      <DescriptionList items={items} />
      {apply && (
        <Box borderWidth="1px" borderColor="border.muted" rounded="md" p="3" aria-label="Boot Media being applied" role="group">
          <BootMediaApplyProgress apply={apply} />
        </Box>
      )}
      {enabled && !image && (
        <Alert status="warning" title="Choose a Boot ISO">
          Boot Media was enabled before Boot ISOs were built in swallow, so it names none. OS deployments of this Server stop at
          the Boot Media step until you choose one with Change ISO.
        </Alert>
      )}
      {enabled && image && !image.available && (
        <Alert status="warning" title="The Boot ISO cannot be mounted">
          {image.reason ?? 'The Boot ISO is not served.'} OS deployments of this Server stop at the Boot Media step until it is
          served again or you choose another with Change ISO.
        </Alert>
      )}
      {setting?.lastError && (
        <Alert status="warning" title={`Last apply failed ${formatRelative(setting.lastErrorAt ?? undefined)}`}>
          {setting.lastError}
        </Alert>
      )}
    </Stack>
  )
}

/**
 * The Boot ISO the setting names: its name and the URL the BMC mounts, or why there is none or it
 * cannot be mounted. A deleted ISO has no name, so its id is shown instead.
 */
function BootISOFact({ image }: { image: ServerBootMedia['image'] }) {
  if (!image) {
    return <Text as="span" color="fg.muted">None chosen — choose one when enabling Boot Media.</Text>
  }
  return (
    <Stack gap="1">
      <HStack gap="2" wrap="wrap">
        <Text as="span" fontWeight="medium">{image.name || image.id}</Text>
        {!image.available && <StatusBadge status="warning" label="Unavailable" />}
      </HStack>
      {image.available ? (
        <HStack gap="1">
          <Text as="span" className="sw-mono" fontSize="sm" wordBreak="break-all">{image.url}</Text>
          <CopyButton value={image.url} label="Copy ISO URL" />
        </HStack>
      ) : (
        <Text as="span" color="fg.muted" fontSize="sm">{image.reason ?? 'The Boot ISO is not served.'}</Text>
      )}
    </Stack>
  )
}

/** The BMC's live answer in words: whether the ISO is mounted and whether the next boot uses it. */
function LiveState({ live }: { live: NonNullable<BootMediaPanelProps['live']> }) {
  if (!live.state) {
    return <Text as="span" color="fg.muted">{live.error ?? 'The BMC did not answer.'}</Text>
  }
  const { mediaInserted, ready, overrideEnabled, overrideTarget } = live.state
  return (
    <Stack gap="1">
      <HStack gap="2" wrap="wrap">
        <StatusBadge status={ready ? 'ready' : 'warning'} label={ready ? 'Next boot starts from the ISO' : 'Next boot does not start from the ISO'} />
      </HStack>
      <Text color="fg.muted" fontSize="sm">
        ISO {mediaInserted ? 'mounted' : 'not mounted'}
        {overrideEnabled ? ` · boot override ${overrideEnabled}${overrideTarget && overrideTarget !== 'None' ? ` → ${overrideTarget}` : ''}` : ''}
      </Text>
    </Stack>
  )
}
