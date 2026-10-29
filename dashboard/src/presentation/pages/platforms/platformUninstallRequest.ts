import type { UninstallPlatformOptions } from '@/domain/platform/types'
import type { ReleaseOptionsValue } from '@/presentation/components/releaseOptions'

/**
 * Builds the uninstall request body from the Uninstall dialog's choices, for the single and bulk
 * dialogs alike, so both send exactly the fields of the platforms.md uninstall contract.
 *
 * Every erase/unbind choice is gated on `releaseServers` so an unchecked release can never submit
 * stray options, and the erase modes only apply when erase itself is on (the standalone Release
 * rule). `cancelActiveWorkflows` and `deleteRecord` are sent only when true, so a plain uninstall
 * keeps the body it always had.
 */
export function platformUninstallOptions(input: {
  releaseServers: boolean
  releaseOptions: ReleaseOptionsValue
  cancelActiveWorkflows: boolean
  deleteRecord: boolean
}): UninstallPlatformOptions {
  const { releaseServers, releaseOptions } = input
  return {
    releaseServers,
    releaseOptions: {
      erase: releaseServers && releaseOptions.erase,
      secureErase: releaseServers && releaseOptions.erase && releaseOptions.secureErase,
      quickErase: releaseServers && releaseOptions.erase && releaseOptions.quickErase,
      unbindStaticIps: releaseServers && releaseOptions.unbindStaticIPs,
    },
    ...(input.cancelActiveWorkflows ? { cancelActiveWorkflows: true } : {}),
    ...(input.deleteRecord ? { deleteRecord: true } : {}),
  }
}
