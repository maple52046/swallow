import { Button } from '@chakra-ui/react'
import type { Operation } from '@/domain/operation/types'
import { Alert } from '@/presentation/components/ui/alert'

interface InspectionAttentionAlertProps {
  /** inspect-hardware Workflows waiting in `requires_attention`; nothing renders when empty. */
  operations: readonly Operation[]
  /** Opens one Workflow, or with no argument the filtered Workflows list. */
  onView: (operationId?: string) => void
}

/**
 * Tells the operator that hardware inspection stopped and what usually fixes it (decision 053).
 * Shared by the Server list (every waiting inspection in the Site scope) and the Server detail
 * page (that Server's inspection).
 *
 * Inspection stops after its bounded attempts, most often because the Server could not
 * network-boot into the provisioner, so the one line names Boot Media and retrying. The
 * Workflow behind the action holds the Server-specific reason. The warning tone is backed by the
 * title text, never colour alone.
 */
export function InspectionAttentionAlert({ operations, onView }: InspectionAttentionAlertProps) {
  if (operations.length === 0) return null
  const single = operations.length === 1 ? operations[0] : undefined
  return (
    <Alert
      status="warning"
      title={single ? 'Hardware inspection needs attention' : `Hardware inspection needs attention on ${operations.length} servers`}
      actions={
        <Button variant="plain" size="sm" alignSelf="center" onClick={() => onView(single?.id)}>
          {single ? 'View workflow' : 'View workflows'}
        </Button>
      }
    >
      If the server can&apos;t PXE boot, enable its Boot Media, then retry.
    </Alert>
  )
}
