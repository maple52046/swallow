import { useState } from 'react'
import { Button, HStack } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ValidationSuite } from '@/domain/validation/types'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useValidationRuns } from '@/presentation/hooks/useValidationRuns'
import { StartValidationRunDialog } from './StartValidationRunDialog'
import { ValidationRunList } from './ValidationRunList'

/** How many of the newest runs the section lists; All runs opens the rest. */
const RECENT_RUNS = 5

type RecentValidationRunsProps = (
  | { serverId: string; platformId?: never }
  | { platformId: string; serverId?: never }
) & {
  /** Whether the user may run a suite on this Server or Platform (Pool operate). */
  canRun: boolean
}

/**
 * The Validation section of a Server's or a Platform's page: its newest Validation Runs (runs
 * whose targets or members include the Server, or the Platform's runs), each linking to its
 * detail, with All runs opening the Runs list filtered to it and, for a user who may operate it, Run
 * a suite opening the run dialog with it as the target. The catalog is read when the dialog opens.
 *
 * The list follows unfinished runs in place (`useValidationRuns`); a failed read is shown with a
 * retry, and a non-admin sees the API's refusal there.
 */
export function RecentValidationRuns({ serverId, platformId, canRun }: RecentValidationRunsProps) {
  const { validation } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const { state, reload } = useValidationRuns({ serverId, platformId, pageSize: RECENT_RUNS })
  const [dialog, setDialog] = useState<{ key: number; suites: ValidationSuite[] } | null>(null)
  const [opening, setOpening] = useState(false)
  const subject = serverId ? 'Server' : 'Platform'
  const allRuns = serverId ? `serverId=${encodeURIComponent(serverId)}` : `platformId=${encodeURIComponent(platformId ?? '')}`

  const openDialog = async () => {
    setOpening(true)
    try {
      const suites = await validation.listSuites()
      // A Server page offers the Servers suites, a Platform page the Platform suites.
      setDialog({ key: Date.now(), suites: suites.filter((suite) => suite.target === (serverId ? 'servers' : 'platform')) })
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The suites could not be loaded', description: failureText(failureOf(caught, '')) })
    } finally {
      setOpening(false)
    }
  }

  return (
    <SectionSurface
      title="Validation"
      description={`The newest Validation Runs of this ${subject}.`}
      actions={
        <HStack gap="2">
          {canRun && <Button size="sm" variant="outline" onClick={() => void openDialog()} loading={opening}>Run a suite</Button>}
          <Button asChild size="sm" variant="ghost">
            <RouterLink to={scopedHref(`/validation/runs?${allRuns}`)}>All runs</RouterLink>
          </Button>
        </HStack>
      }
    >
      {state.status === 'loading' && <LoadingState rows={2} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />}
      {state.status === 'ready' && state.page.items.length === 0 && (
        <EmptyState title="No Validation Runs" message={`No suite has validated this ${subject} yet.`} />
      )}
      {state.status === 'ready' && state.page.items.length > 0 && (
        <ValidationRunList runs={state.page.items} label={`Validation Runs of this ${subject}`} hideTarget={Boolean(platformId)} />
      )}
      {/* A new key per opening resets the form; the dialog stays mounted so it can animate closed. */}
      <StartValidationRunDialog
        key={dialog?.key ?? 0}
        open={dialog !== null}
        onClose={() => setDialog(null)}
        suites={dialog?.suites ?? []}
        preset={{ serverIds: serverId ? [serverId] : undefined, platformId }}
      />
    </SectionSurface>
  )
}
