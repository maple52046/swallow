import type { ReactNode } from 'react'
import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { operationStatus, type Operation } from '@/domain/operation/types'
import { Alert } from '@/presentation/components/ui/alert'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { serverActivityPath } from './serverActivitySections'
import type { ServerReleaseActivity } from './useServerReleaseActivity'

/** Why lifecycle controls are disabled, shown in every variant so the operator need not guess. */
const STATE_LOCK_NOTE =
  'Swallow cannot change the state of a Server the provider is releasing: Release, Recover, Lock, and Deploy OS stay unavailable until the provider returns it to Ready or reports a failure.'

/** The Activity sections the banner links to, labelled by where each link lands. */
const ACTIVITY_LINK_LABELS = {
  'related-operations': 'View Related Operations',
  'provider-events': 'View provider events',
} as const

interface ReleaseInProgressAlertProps {
  serverId: string
  /** From `useServerReleaseActivity`; nothing renders while it is `idle` or `loading`. */
  activity: ServerReleaseActivity
}

/** The message an attention-parked Workflow stopped with, from its first Task needing attention. */
function attentionMessage(operation: Operation): string | undefined {
  return operation.steps?.find((step) => step.status === 'requires_attention' && step.error)?.error?.message
}

/**
 * Explains a Server that shows Releasing on its detail page and links to the record of that work,
 * which the Server projection does not reference directly. Without it the operator sees a
 * Releasing badge, disabled lifecycle actions, and no hint of where the release is tracked.
 *
 * - A Workflow following the release (info): links to it and to Activity → Related Operations.
 * - A Workflow parked for attention (warning): shows why it stopped and links to it and to
 *   Provider events, where the provider's own progress is.
 * - A Workflow that ended while the provider still releases (warning): says nothing in Swallow
 *   follows the release any more and links to it and to Provider events.
 * - No Workflow (info): the release was started in the provisioner or through the deprecated
 *   synchronous endpoint, so only Provider events record it.
 * - The Workflows could not be read (warning): shows the error and links to Related Operations.
 *
 * Every variant states why the Server's state cannot be changed meanwhile. The tone is backed by
 * the title text, never colour alone, and the actions are links named after their destination.
 */
export function ReleaseInProgressAlert({ serverId, activity }: ReleaseInProgressAlertProps) {
  const { scopedHref } = useSiteScope()
  if (activity.status === 'idle' || activity.status === 'loading') return null

  const workflowLink = (operation: Operation) => (
    <Button asChild variant="plain" size="sm" px="0" h="auto" colorPalette="brand">
      <RouterLink to={scopedHref(`/workflows/${operation.id}`)}>View workflow</RouterLink>
    </Button>
  )
  const activityLink = (section: keyof typeof ACTIVITY_LINK_LABELS) => (
    <Button asChild variant="plain" size="sm" px="0" h="auto" colorPalette="brand">
      <RouterLink to={scopedHref(serverActivityPath(serverId, section))}>{ACTIVITY_LINK_LABELS[section]}</RouterLink>
    </Button>
  )

  if (activity.status === 'error') {
    return (
      <ReleaseAlertFrame status="warning" title="Releasing the Server" links={activityLink('related-operations')}>
        <Text>Swallow could not read this Server's Workflows: {activity.message}</Text>
      </ReleaseAlertFrame>
    )
  }

  const workflow = activity.workflow
  if (!workflow) {
    return (
      <ReleaseAlertFrame status="info" title="The provider is releasing this Server" links={activityLink('provider-events')}>
        <Text>
          No Swallow Workflow covers this release: it was started in the provisioner or through the
          deprecated release endpoint. Provider events record its progress.
        </Text>
      </ReleaseAlertFrame>
    )
  }

  const { operation, phase } = workflow
  const intent = operation.intent || 'The release Workflow'
  if (phase === 'in-flight') {
    return (
      <ReleaseAlertFrame
        status="info"
        title="Releasing the Server"
        links={<>{workflowLink(operation)}{activityLink('related-operations')}</>}
      >
        <Text>
          {intent} is following the provider until the Server is back in the ready pool. Disk
          erasure can take a long time.
        </Text>
      </ReleaseAlertFrame>
    )
  }
  if (phase === 'attention') {
    const reason = attentionMessage(operation)
    return (
      <ReleaseAlertFrame
        status="warning"
        title="Release needs attention"
        links={<>{workflowLink(operation)}{activityLink('provider-events')}</>}
      >
        <Text>
          {reason ? `${intent} stopped following the provider: ${reason}` : `${intent} stopped waiting for the Server to return to Ready.`}{' '}
          The provider still reports Releasing. Check Provider events for its progress, then retry
          the Task from the Workflow once the provider settles.
        </Text>
      </ReleaseAlertFrame>
    )
  }
  return (
    <ReleaseAlertFrame
      status="warning"
      title="No Workflow is following this release"
      links={<>{workflowLink(operation)}{activityLink('provider-events')}</>}
    >
      <Text>
        {intent} ended ({operationStatus(operation).replaceAll('_', ' ')}) while the provider still
        reports Releasing. Provider events show whether the release is still advancing.
      </Text>
    </ReleaseAlertFrame>
  )
}

/**
 * The layout every variant shares: the variant's explanation, the state-lock note, then its links
 * in a wrapping row so two links fit a narrow viewport. The banner is a polite `status` region
 * because the polling hook can change its variant (for example to "Release needs attention")
 * while the operator is on the page.
 */
function ReleaseAlertFrame({
  status,
  title,
  links,
  children,
}: {
  status: 'info' | 'warning'
  title: string
  links: ReactNode
  children: ReactNode
}) {
  return (
    <Alert status={status} title={title} role="status">
      <Stack gap="1">
        {children}
        <Text color="fg.muted">{STATE_LOCK_NOTE}</Text>
        <HStack gap="4" wrap="wrap">
          {links}
        </HStack>
      </Stack>
    </Alert>
  )
}
