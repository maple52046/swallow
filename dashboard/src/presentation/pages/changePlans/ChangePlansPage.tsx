import { useCallback, useEffect, useRef, useState } from 'react'
import { Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ChangePlan } from '@/domain/convergence/changePlan'
import { canOperatePool, isAdmin } from '@/domain/user/access'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { usePlatformPools } from '@/presentation/hooks/usePlatformPools'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'
import { ActivityHeader } from '@/presentation/pages/operations/ActivityHeader'
import { ChangePlanCard } from './ChangePlanCard'
import { ChangePlanPolicyCard } from './ChangePlanPolicyCard'
import {
  changePlanApprovalConsequence,
  changePlanApprovalScope,
  changePlanResourceIds,
  changePlanResourceLabel,
  groupChangePlans,
  type ChangePlanNames,
} from './changePlanPresentation'
import { useChangePlanNames } from './useChangePlanNames'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; plans: ChangePlan[] }

/** The decision being confirmed; at most one at a time. */
type Decision = { kind: 'approve' | 'reject'; plan: ChangePlan } | null

const NO_PLANS: ChangePlan[] = []
const NO_IDS: string[] = []

/**
 * The Change Plans tab of Activity (route `/change-plans`, decision 076): every destructive change's plan
 * is listed here. A change the user confirmed when submitting it arrives approved and is applied by
 * its controller; a plan that waits — submitted without approval, blocked, proposed by Swallow, or
 * held by the Change Plan policy — is approved or rejected here as a whole. The policy's switch
 * (`ChangePlanPolicyCard`) sits above the plans.
 *
 * Data comes from the `change-plans.md` contract through the ChangePlanRepository port, read on
 * each visit and after each decision; the page shows what the API computed and derives nothing.
 * While any plan is `approved` — waiting for its controller to apply or supersede it — the list is
 * re-read every few seconds (`useSettlingRefresh`) in place, without returning to the loading
 * state, so the plan moves on to its end state without a manual reload; a failed re-read keeps
 * what is shown. Platform and Server ids are shown by name through `useChangePlanNames`, falling
 * back to the id.
 *
 * Plans are grouped as waiting for approval, approved, and recent final plans. Approving or
 * rejecting always goes through a confirmation that names the resource and the consequence and
 * opens on Cancel; a `conflict` from the API (for example a plan that expired meanwhile) is shown
 * as a toast and the list re-read. A user who is not an admin sees the plans of the Platforms its
 * Pools hold without the policy switch, which is admin-only, and is offered Approve and Reject only
 * for a Platform whose Pool it owns.
 */
export function ChangePlansPage() {
  const { changePlans } = useApp()
  const { currentUser } = useAuth()
  // The Change Plan policy is an admin read and write (change-plans.md); other users see the plans
  // of their Pools' Platforms without it.
  const admin = isAdmin(currentUser)
  const { showToast } = useToast()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [decision, setDecision] = useState<Decision>(null)
  const [busy, setBusy] = useState(false)
  const [reloadNonce, setReloadNonce] = useState(0)
  // Captured with each read rather than during render, so rendering stays pure.
  const [readAt, setReadAt] = useState(0)
  // Every read takes the next ticket and only the newest ticket may publish, so a slow poll can
  // never overwrite a newer read, and a read whose effect was cleaned up is dropped.
  const readTicket = useRef(0)

  const plans = state.status === 'ready' ? state.plans : NO_PLANS
  const names = useChangePlanNames(plans)
  // A Platform's plan is decided by an admin or an owner of the Platform's Pool; plans of other
  // resource kinds by an admin only (change-plans.md).
  const platformPools = usePlatformPools(admin ? NO_IDS : changePlanResourceIds(plans, 'platform'))
  const canDecide = (plan: ChangePlan) =>
    admin || (plan.resource.kind === 'platform' && canOperatePool(currentUser, platformPools.get(plan.resource.id)))

  /** Re-reads the list and shows it in place; rejects with the read's failure for the caller to handle. */
  const refresh = useCallback(async () => {
    const ticket = ++readTicket.current
    const next = await changePlans.listChangePlans()
    if (ticket !== readTicket.current) return
    setReadAt(Date.now())
    setState({ status: 'ready', plans: next })
  }, [changePlans])

  // The visit's first read, and the re-read after a decision or a retry. Its failure is shown only
  // while it is still the newest read; `refresh` takes this ticket synchronously.
  useEffect(() => {
    const ticket = readTicket.current + 1
    refresh().catch((caught: unknown) => {
      if (ticket === readTicket.current) setState({ status: 'error', ...failureOf(caught, 'Could not load Change Plans.') })
    })
    return () => {
      readTicket.current += 1
    }
  }, [refresh, reloadNonce])

  // An approved plan waits for its controller, so re-read until none is left approved.
  useSettlingRefresh(plans.some((plan) => plan.state === 'approved'), refresh)

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
        description: changePlanResourceLabel(decision.plan.resource, names),
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
      <ActivityHeader subtitle="What each destructive change does, who approved it, and the plans that still wait for approval." />

      {admin && <ChangePlanPolicyCard />}
      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />}
      {state.status === 'ready' && (
        <ChangePlanSections
          plans={state.plans}
          names={names}
          now={readAt}
          canDecide={canDecide}
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
            <Text>{changePlanApprovalScope(decision.plan, names)}</Text>
            <Text>{changePlanApprovalConsequence(decision.plan)}</Text>
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
            Nothing in this plan for {changePlanResourceLabel(decision.plan.resource, names)} will be carried out. Make the
            change again, for example Uninstall again, to get a new plan.
          </Text>
        )}
      </ConfirmDialog>
    </div>
  )
}

/** The three plan sections; an empty installation shows one explanatory empty state. */
function ChangePlanSections({ plans, names, now, canDecide, onApprove, onReject }: {
  plans: ChangePlan[]
  names: ChangePlanNames
  now: number
  canDecide: (plan: ChangePlan) => boolean
  onApprove: (plan: ChangePlan) => void
  onReject: (plan: ChangePlan) => void
}) {
  if (plans.length === 0) {
    return (
      <SectionSurface title="Change Plans">
        <EmptyState
          title="No Change Plans"
          message="A destructive change, such as uninstalling a Platform, records its plan here; a plan that waits for approval is decided here."
        />
      </SectionSurface>
    )
  }
  const groups = groupChangePlans(plans)
  const section = (title: string, description: string, items: ChangePlan[]) => items.length > 0 && (
    <SectionSurface title={title} description={description}>
      <Stack gap="4">
        {items.map((plan) => (
          <ChangePlanCard key={plan.id} plan={plan} names={names} now={now} canDecide={canDecide(plan)} onApprove={onApprove} onReject={onReject} />
        ))}
      </Stack>
    </SectionSurface>
  )
  return (
    <Stack gap="6">
      {section('Waiting for approval', 'Each plan says why it waits. Review each action and its effect before approving the plan as a whole.', groups.pending)}
      {section(
        'Approved',
        'Each resource’s controller recomputes the plan and applies it, or supersedes it if the spec or observed state changed; a superseded plan that still changes something comes back here for approval. This list refreshes on its own until then.',
        groups.approved,
      )}
      {section('Recent plans', 'Applied, rejected, superseded, and expired plans.', groups.finished)}
    </Stack>
  )
}
