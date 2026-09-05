import {
  Alert,
  AlertVariant,
  Button,
  ExpandableSection,
  Stack,
  StackItem,
} from '@patternfly/react-core'
import type { DeploymentAxis } from '@/domain/server/types'
import { deploymentFailureSummary } from './deploymentFailure'

/**
 * Presents a failed or attention-needing deployment on the Server detail page as a concise
 * root cause plus a link to the owning Operation. The verbose executor reason is kept out of
 * the way behind a collapsible "Show details", so the common case ("no network address") is
 * one readable line rather than a wall of recovery text.
 */
export function DeploymentFailureAlert({
  deployment,
  onViewOperation,
}: {
  deployment: DeploymentAxis
  onViewOperation: () => void
}) {
  const failed = deployment.state === 'failed'
  const summary = deploymentFailureSummary(deployment)
  const reason = deployment.statusReason.trim()
  // Only offer details when the full reason adds something beyond the concise summary.
  const showDetails = reason !== '' && reason !== summary.trim()

  return (
    <Alert
      variant={failed ? AlertVariant.danger : AlertVariant.warning}
      title={
        failed
          ? 'Operating system deployment failed'
          : 'Operating system deployment requires attention'
      }
      isInline
    >
      <Stack hasGutter>
        <StackItem>
          {summary}{' '}
          <Button variant="link" isInline onClick={onViewOperation}>
            View operation
          </Button>
        </StackItem>
        {showDetails && (
          <StackItem>
            <ExpandableSection toggleText="Show details" toggleTextExpanded="Hide details">
              {reason}
            </ExpandableSection>
          </StackItem>
        )}
      </Stack>
    </Alert>
  )
}
