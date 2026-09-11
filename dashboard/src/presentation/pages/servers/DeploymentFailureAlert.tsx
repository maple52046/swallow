import { useState } from 'react'
import { Box, Button, Stack, Text } from '@chakra-ui/react'
import type { DeploymentAxis } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { deploymentFailureSummary } from './deploymentFailure'

/**
 * Presents a failed or attention-needing deployment on the Server detail page as a concise
 * root cause plus a link to the owning Operation. The verbose executor reason is kept behind a
 * collapsible "Show details", so the common case ("no network address") is one readable line
 * rather than a wall of recovery text.
 */
export function DeploymentFailureAlert({
  deployment,
  onViewOperation,
}: {
  deployment: DeploymentAxis
  onViewOperation: () => void
}) {
  const [open, setOpen] = useState(false)
  const failed = deployment.state === 'failed'
  const summary = deploymentFailureSummary(deployment)
  const reason = deployment.statusReason.trim()
  // Only offer details when the full reason adds something beyond the concise summary.
  const showDetails = reason !== '' && reason !== summary.trim()

  return (
    <Alert
      status={failed ? 'error' : 'warning'}
      title={failed ? 'Operating system deployment failed' : 'Operating system deployment requires attention'}
    >
      <Stack gap="2">
        <Box>
          {summary}{' '}
          <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={onViewOperation}>
            View operation
          </Button>
        </Box>
        {showDetails && (
          <Box>
            <Button variant="plain" size="sm" px="0" h="auto" onClick={() => setOpen((value) => !value)}>
              {open ? 'Hide details' : 'Show details'}
            </Button>
            {open && (
              <Text mt="1" whiteSpace="pre-wrap" className="sw-error-detail">
                {reason}
              </Text>
            )}
          </Box>
        )}
      </Stack>
    </Alert>
  )
}
