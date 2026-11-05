import { Stack, Text } from '@chakra-ui/react'
import type { Platform } from '@/domain/platform/types'
import type { Integration } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'

/**
 * Tells an admin, in the Overview's Cluster access section, that a Kubernetes Platform still
 * reaches its cluster with the one `cluster-admin` token a deployment before 2026-11-05 recorded,
 * for reads and writes alike, and that its next successful deployment Workflow narrows it
 * (platforms-kubernetes.md "Cluster Credentials").
 *
 * `integration` is the Platform's credential Integration as read for its current id; the caller
 * renders the notice only once that read succeeded, so an unknown credential layout is never
 * presented as the single-token one. It renders only when the Integration reports
 * `hasOperatorCredential: false`. The explorer works either way, so the notice is informational. A
 * failed latest deployment adds a sentence: one that failed after narrowing the token leaves
 * cluster changes refused until a deployment succeeds, which the Integration alone cannot tell
 * apart.
 */
export function SingleClusterCredentialNotice({ platform, integration }: { platform: Platform; integration: Integration }) {
  if (integration.hasOperatorCredential) return null
  return (
    <Alert status="info" title="Cluster credentials are not separated yet">
      <Stack gap="1">
        <Text>
          This Platform was deployed before Swallow separated read and write cluster credentials, so Swallow still uses its one
          cluster-admin token for both: membership sync, the cluster explorer&apos;s reads, and the changes an admin makes there.
        </Text>
        <Text>
          The next deployment Workflow on this Platform that succeeds narrows that token to read-only and records a separate
          cluster-admin token used only for changes.
        </Text>
        {platform.lifecycleState === 'deploy_failed' && (
          <Text>
            The latest deployment failed. If it failed after narrowing the token, cluster changes are refused until a deployment
            succeeds; reads keep working.
          </Text>
        )}
      </Stack>
    </Alert>
  )
}
