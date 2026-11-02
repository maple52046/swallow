import { useCallback, useEffect, useState } from 'react'
import { Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ChangePlan } from '@/domain/convergence/changePlan'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useToast } from '@/presentation/components/toast/toastContext'
import { ChangePlanCard } from './ChangePlanCard'
import { changePlanResourceLabel, destructiveActionCount, groupChangePlans } from './changePlanPresentation'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; plans: ChangePlan[] }

/** The decision being confirmed; at most one at a time. */
type Decision = { kind: 'approve' | 'reject'; plan: ChangePlan } | null

/**
 * The Change Plans workspace (route `/change-plans`, decision 076): destructive changes wait here
 * until an admin approves or rejects them as a whole.
 *
 * Data comes from the `change-plans.md` contract through the ChangePlanRepository port, read once
 * per visit and after each decision; the page shows what the API computed and derives nothing.
 * Plans are grouped as waiting for approval, approved (waiting for their controller, which may
 * still supersede them or wait out a hold), and recent final plans. Approving or rejecting always
 * goes through a confirmation that names the resource and the consequence; a `conflict` from the
 * API (for example a plan that expired meanwhile) is shown as a toast and the list re-read. Every
 * endpoint requires `admin`; a non-admin sees the API's error state.
 */
export function ChangePlansPage() {
  const { changePlans } = useApp()
  const { showToast } = useToast()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [decision, setDecision] = useState<Decision>(null)
  const [busy, setBusy] = useState(false)
  const [reloadNonce, setReloadNonce] = useState(0)
  // Captured with each read rather than during render, so rendering stays pure.
  const [readAt, setReadAt] = useState(0)

  // The `cancelled` flag drops a slower, older response so it cannot overwrite a newer one.
  useEffect(() => {
    let cancelled = false
    void changePlans.listChangePlans()
      .then((plans) => {
        if (cancelled) return
        setReadAt(Date.now())
        setState({ status: 'ready', plans })
      })
      .catch((caught: unknown) => {
        if (!cancelled) setState({ status: 'error', ...failureOf(caught, 'Could not load Change Plans.') })
      })
    return () => {
      cancelled = true
    }
  }, [changePlans, reloadNonce])

  const reload = useCallback(() => {
    setState({ status: 'loading' })
    setReloadNonce((value) => value + 1)
  }, [])

  const decide = async () => {
    if (!decision) return
    setBusy(true)
    try {
      if (decision.kind === 'approve') await changePlans.approveChangePlan(decision.plan.id)
      else await changePlans.rejectChangePlan(decision.plan.id)
      showToast({
        tone: 'success',
        title: decision.kind === 'approve' ? 'Change Plan approved' : 'Change Plan rejected',
        description: changePlanResourceLabel(decision.plan.resource),
      })
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The decision was not recorded', description: failureText(failureOf(caught, '')) })
    } finally {
      setBusy(false)
      setDecision(null)
      setReloadNonce((value) => value + 1)
    }
  }

  return (
    <div className="console-page">
      <PageHeader
        title="Change Plans"
        subtitle="Destructive changes wait here until an admin approves them as a whole."
      />

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />}
      {state.status === 'ready' && (
        <ChangePlanSections
          plans={state.plans}
          now={readAt}
          onApprove={(plan) => setDecision({ kind: 'approve', plan })}
          onReject={(plan) => setDecision({ kind: 'reject', plan })}
        />
      )}

      <ConfirmDialog
        open={decision?.kind === 'approve'}
        title="Approve Change Plan"
        confirmLabel="Approve plan"
        busy={busy}
        onConfirm={() => void decide()}
        onCancel={() => setDecision(null)}
      >
        {decision && (
          <Stack gap="2">
            <Text>
              Swallow will carry out every action of this plan for {changePlanResourceLabel(decision.plan.resource)},
              including {destructiveActionCount(decision.plan)} destructive action(s).
            </Text>
            <Text>
              An approved plan cannot be changed. Once its Workflow runs, destructive actions cannot be undone; if the spec
              changes first, the plan is superseded instead.
            </Text>
          </Stack>
        )}
      </ConfirmDialog>
      <ConfirmDialog
        open={decision?.kind === 'reject'}
        title="Reject Change Plan"
        confirmLabel="Reject plan"
        busy={busy}
        onConfirm={() => void decide()}
        onCancel={() => setDecision(null)}
      >
        {decision && (
          <Text>
            Nothing in this plan for {changePlanResourceLabel(decision.plan.resource)} will be carried out. Change the spec again to get a new plan.
          </Text>
        )}
      </ConfirmDialog>
    </div>
  )
}

/** The three plan sections; an empty installation shows one explanatory empty state. */
function ChangePlanSections({ plans, now, onApprove, onReject }: {
  plans: ChangePlan[]
  now: number
  onApprove: (plan: ChangePlan) => void
  onReject: (plan: ChangePlan) => void
}) {
  if (plans.length === 0) {
    return (
      <SectionSurface title="Change Plans">
        <EmptyState
          title="No Change Plans"
          message="A destructive change, such as uninstalling a Platform, creates a plan here for approval."
        />
      </SectionSurface>
    )
  }
  const groups = groupChangePlans(plans)
  const section = (title: string, description: string, items: ChangePlan[]) => items.length > 0 && (
    <SectionSurface title={title} description={description}>
      <Stack gap="4">
        {items.map((plan) => <ChangePlanCard key={plan.id} plan={plan} now={now} onApprove={onApprove} onReject={onReject} />)}
      </Stack>
    </SectionSurface>
  )
  return (
    <Stack gap="6">
      {section('Waiting for approval', 'Review each action and its effect before approving the plan as a whole.', groups.pending)}
      {section('Approved', 'Each resource’s controller recomputes the plan and applies it, or supersedes it if the spec or observed state changed.', groups.approved)}
      {section('Recent plans', 'Applied, rejected, superseded, and expired plans.', groups.finished)}
    </Stack>
  )
}
