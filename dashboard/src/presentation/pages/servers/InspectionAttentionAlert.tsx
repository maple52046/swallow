import { Button, HStack } from '@chakra-ui/react'
import type { Operation } from '@/domain/operation/types'
import { Alert } from '@/presentation/components/ui/alert'

/**
 * The attention code the enrollment wait stops with when the provisioner cannot observe the power-off
 * that ends enrollment (contract server-enrollment.md, decision 054). Branching on the published code,
 * never on the message text.
 */
const POWER_CONFIGURATION_REQUIRED = 'power_configuration_required'

interface InspectionAttentionAlertProps {
  /** inspect-hardware Workflows waiting in `requires_attention`; nothing renders when empty. */
  operations: readonly Operation[]
  /** Opens one Workflow, or with no argument the filtered Workflows list. */
  onView: (operationId?: string) => void
  /**
   * Opens one Server's Summary, where its Power Configuration is set. Offered only for a single
   * waiting inspection that stopped for a missing Power Configuration; omit it on a page that is
   * already that Summary.
   */
  onConfigurePower?: (serverId: string) => void
}

/** Whether the Workflow's attention is the missing Power Configuration, by its Task's error code. */
function needsPowerConfiguration(operation: Operation): boolean {
  return (operation.steps ?? []).some(
    (step) => step.status === 'requires_attention' && step.error?.code === POWER_CONFIGURATION_REQUIRED,
  )
}

/**
 * Tells the operator that hardware inspection stopped and what usually fixes it (decisions 053 and
 * 054). Shared by the Server list (every waiting inspection in the Site scope) and the Server detail
 * page (that Server's inspection).
 *
 * The one line names the fix that matches why inspection stopped: a Server whose power the
 * provisioner cannot read (a libvirt VM without a driver, typically) needs its Power Configuration,
 * and one that could not network-boot needs Boot Media; both then retry. The Workflow behind the
 * action holds the Server-specific reason. The warning tone is backed by the title text, never
 * colour alone.
 */
export function InspectionAttentionAlert({ operations, onView, onConfigurePower }: InspectionAttentionAlertProps) {
  if (operations.length === 0) return null
  const single = operations.length === 1 ? operations[0] : undefined
  const power = operations.filter(needsPowerConfiguration).length
  const powerServerId = single && power === 1 ? single.targetServerIds[0] : undefined
  const guidance =
    power === 0
      ? "If the server can't PXE boot, enable its Boot Media, then retry."
      : power === operations.length
        ? `The provisioner cannot see enrollment end because ${single ? 'the server has' : 'these servers have'} no power driver it can read. Set the power configuration on the server's Summary (for a libvirt VM: virsh with its hypervisor URI and domain), then retry.`
        : "Set the power configuration of servers without power control, enable Boot Media on servers that can't PXE boot, then retry."
  return (
    <Alert
      status="warning"
      title={single ? 'Hardware inspection needs attention' : `Hardware inspection needs attention on ${operations.length} servers`}
      actions={
        <HStack gap="1" alignSelf="center">
          {powerServerId && onConfigurePower && (
            <Button variant="plain" size="sm" onClick={() => onConfigurePower(powerServerId)}>
              Set power configuration
            </Button>
          )}
          <Button variant="plain" size="sm" onClick={() => onView(single?.id)}>
            {single ? 'View workflow' : 'View workflows'}
          </Button>
        </HStack>
      }
    >
      {guidance}
    </Alert>
  )
}
