import { useState } from 'react'
import { Button, Stack, Text } from '@chakra-ui/react'
import { RefreshCw } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { DirectoryStatus } from '@/domain/access/accounts'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { ConvergenceStatus } from '@/presentation/components/convergence/ConvergenceStatus'
import { ErrorState } from '@/presentation/components/ErrorState'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { formatDateTime } from '@/shared/utils/time'

/**
 * The installation's Directory on the Users page (access.md, admins only): where the Servers'
 * directory client connects, whether the Directory controller's last apply succeeded, and how much
 * it holds. Without a Directory the section says so instead of showing empty counts, since an
 * installation without one keeps Servers on the Deployment Key and admins' keys only.
 *
 * "Apply now" records a request and returns at once; the controller applies within moments, so the
 * section re-reads once after the request rather than polling.
 */
export function DirectorySection() {
  const { accounts } = useApp()
  const { showToast } = useToast()
  const directory = useAsyncData(() => accounts.getDirectory(), [accounts], { unavailableStatuses: [] })
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const sync = async () => {
    setSyncing(true)
    setError(null)
    try {
      await accounts.syncDirectory()
      showToast({ tone: 'success', title: 'Directory apply requested' })
      directory.reload()
    } catch (caught) {
      setError(failureOf(caught, 'The Directory apply could not be requested.'))
    } finally {
      setSyncing(false)
    }
  }

  const configured = directory.status === 'ready' && directory.data.configured
  const action = configured ? (
    <Button size="sm" variant="outline" onClick={() => void sync()} loading={syncing}>
      <RefreshCw size={14} aria-hidden />
      Apply now
    </Button>
  ) : undefined

  return (
    <SectionSurface
      title="Directory"
      description="Accounts, groups, SSH keys, and sudo rules the Servers read through their directory client."
      actions={action}
    >
      {directory.status === 'loading' && <LoadingState rows={3} />}
      {(directory.status === 'error' || directory.status === 'unavailable') && (
        <ErrorState message={directory.message} requestId={directory.status === 'error' ? directory.requestId : undefined} onRetry={directory.reload} />
      )}
      {directory.status === 'ready' && (
        <Stack gap="4">
          {error && (
            <Alert status="error" title="Apply not requested">
              <FailureMessage failure={error} />
            </Alert>
          )}
          {directory.data.configured ? (
            <DirectoryDetails status={directory.data} />
          ) : (
            <Text color="fg.muted">
              This installation has no Directory. Users can sign in to Swallow, but only admins&apos; SSH keys reach the Servers.
            </Text>
          )}
        </Stack>
      )}
    </SectionSurface>
  )
}

/** The configured Directory's status and counts; counts render as text so a zero reads as zero. */
function DirectoryDetails({ status }: { status: DirectoryStatus }) {
  return (
    <DescriptionList
      items={[
        { label: 'Status', value: <ConvergenceStatus conditions={status.conditions} generation={0} /> },
        { label: 'Servers connect to', value: <Text as="span" className="sw-mono">{status.hostUrl}</Text> },
        { label: 'Base DN', value: <Text as="span" className="sw-mono">{status.baseDn}</Text> },
        { label: 'Last applied', value: status.lastAppliedAt ? formatDateTime(status.lastAppliedAt) : 'Not applied yet' },
        { label: 'Contents', value: `${status.users} users, ${status.groups} groups, ${status.sudoRules} sudo rules, ${status.hosts} hosts` },
      ]}
    />
  )
}
