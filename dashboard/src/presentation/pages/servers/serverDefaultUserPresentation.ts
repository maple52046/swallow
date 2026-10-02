import type { SetServerDefaultUserResult } from '@/domain/server/types'
import type { ToastTone } from '@/presentation/components/toast/toastContext'

/**
 * The POSIX login-name rule api-server enforces for a Server Default User (contract
 * server-detail-actions.md). Mirrored only so the dialog can reject a typo before a multi-second
 * host login; the API stays authoritative.
 */
export const DEFAULT_USER_PATTERN = /^[a-z_][a-z0-9_-]{0,31}$/

/**
 * The one-line command an operator runs as the account on the host to authorize the Deployment Key
 * without giving swallow a password. It creates `~/.ssh` and `authorized_keys` with the permissions
 * sshd requires. `publicKey` is the Deployment Key's authorized_keys line from the SSH Keys API; it
 * never contains a single quote, so single-quoting it is safe.
 */
export function deploymentKeyInstallCommand(publicKey: string): string {
  return `mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '${publicKey.trim()}' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys`
}

/**
 * The toast for a saved default user. Sudo access decides the tone: automation needs root for host
 * changes, so an account that needs a password (the Site become password applies) or cannot use
 * sudo is a warning the operator must act on, not a plain success.
 */
export function defaultUserSavedToast(result: SetServerDefaultUserResult): { tone: ToastTone; title: string; description: string } {
  const user = result.defaultUser.user
  const installed = result.keyInstalled ? ` The Deployment Key was added to ${user}'s authorized_keys.` : ''
  const title = `Default user set to ${user}`
  switch (result.sudo) {
    case 'passwordless':
      return { tone: 'success', title, description: `swallow logs in as ${user}, and sudo works without a password.${installed}` }
    case 'password_required':
      return {
        tone: 'warning',
        title,
        description: `${user} needs a password for sudo. Automation uses the Site's become password, so set it in the Site's automation settings if it is missing.${installed}`,
      }
    case 'unavailable':
      return {
        tone: 'warning',
        title,
        description: `${user} cannot use sudo, so software installs and Platform deploys on this Server will fail until it can.${installed}`,
      }
  }
}
