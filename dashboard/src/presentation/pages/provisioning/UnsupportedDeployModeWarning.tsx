import { DEPLOY_TARGET_LABELS, type DeployTarget } from '@/domain/provisioning/types'
import { Alert } from '@/presentation/components/ui/alert'

interface UnsupportedDeployModeWarningProps {
  imageName: string
  deployTarget: DeployTarget
  className?: string
}

/** Warns about an unverified image/mode pair without enforcing deployment eligibility. */
export function UnsupportedDeployModeWarning({
  imageName,
  deployTarget,
  className,
}: UnsupportedDeployModeWarningProps) {
  return (
    <Alert
      status="warning"
      title="This image does not support this deploy mode"
      className={className}
    >
      <strong>{imageName}</strong> has not been verified for{' '}
      {DEPLOY_TARGET_LABELS[deployTarget]}. You can continue, but deployment may fail.
    </Alert>
  )
}
