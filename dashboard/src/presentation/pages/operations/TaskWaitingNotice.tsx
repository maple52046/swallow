import { Text } from '@chakra-ui/react'
import type { OperationStepWaiting } from '@/domain/operation/types'
import { Alert } from '@/presentation/components/ui/alert'
import { formatDateTime } from '@/shared/utils/time'
import { waitingProgressText } from './taskDiagnosticPresentation'

/**
 * What a running Step waits for on the Workflow detail page: the latest observation its Runner
 * published (`waiting`, workflows contract), how many observations it made, and its elapsed time
 * against the wait's budget with what is left. The numbers are the Runner's at `observedAt`, shown
 * with that time, rather than a live clock: they refresh with the page's polling, so the notice
 * never claims progress swallow did not observe. It is advisory and informational (an `info`
 * alert with a text title), and it is not a live region, so polling does not re-announce it.
 */
export function TaskWaitingNotice({ waiting }: { waiting: OperationStepWaiting }) {
  return (
    <Alert status="info" title={`Waiting: ${waiting.observation}`}>
      <Text>
        {waitingProgressText(waiting)} · last observed {formatDateTime(waiting.observedAt)}
      </Text>
    </Alert>
  )
}
