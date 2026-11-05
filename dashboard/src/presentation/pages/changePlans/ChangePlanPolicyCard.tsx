import { useCallback, useEffect, useId, useState } from 'react'
import { Button, Stack, Switch, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { ChangePlanPolicy } from '@/domain/convergence/changePlan'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { Alert } from '@/presentation/components/ui/alert'
import { useToast } from '@/presentation/components/toast/toastContext'
import { formatDateTime } from '@/shared/utils/time'

type PolicyState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; policy: ChangePlanPolicy }

/**
 * The safety switch on the Change Plans page: the installation's Change Plan policy (contract
 * `change-plans.md`, "Change Plan Policy"), read through the ChangePlanRepository port when the
 * page opens.
 *
 * The switch reads as the safety switch — on means every Change Plan waits here for a separate
 * approval — so it is checked when the policy's `approveOnSubmit` is false. Flipping it writes the
 * policy at once (the API records the admin) and applies to the changes submitted afterwards; plans
 * already listed keep their state, which the copy says. While the write is in flight the switch is
 * disabled; a refused write keeps the previous value and shows the failure in the card. A failed
 * read replaces the switch with the failure and a retry, so the page never shows a policy it did
 * not read. Every route needs `admin`, like the rest of the page.
 */
export function ChangePlanPolicyCard() {
  const { changePlans } = useApp()
  const { showToast } = useToast()
  const [state, setState] = useState<PolicyState>({ status: 'loading' })
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<DisplayedFailure | null>(null)
  const [readNonce, setReadNonce] = useState(0)
  const descriptionId = useId()

  // One read per visit and per retry; a read that finishes after the card unmounted, or after a
  // newer read started, is dropped.
  useEffect(() => {
    let current = true
    changePlans.getChangePlanPolicy().then(
      (policy) => { if (current) setState({ status: 'ready', policy }) },
      (caught: unknown) => { if (current) setState({ status: 'error', ...failureOf(caught, 'Could not load the Change Plan policy.') }) },
    )
    return () => { current = false }
  }, [changePlans, readNonce])

  const retry = useCallback(() => {
    setState({ status: 'loading' })
    setReadNonce((value) => value + 1)
  }, [])

  const setRequireApproval = async (requireApproval: boolean) => {
    setSaving(true)
    setSaveError(null)
    try {
      const policy = await changePlans.setChangePlanPolicy(!requireApproval)
      setState({ status: 'ready', policy })
      showToast({
        tone: 'success',
        title: requireApproval ? 'Separate approval required' : 'Confirmed changes start at once',
        description: requireApproval
          ? 'From now on every Change Plan waits here for approval.'
          : 'From now on a change confirmed when it is submitted is approved with it.',
      })
    } catch (caught: unknown) {
      const failure = failureOf(caught, 'Could not change the Change Plan policy.')
      setSaveError(failure)
      showToast({ tone: 'error', title: 'The policy was not changed', description: failureText(failure) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionSurface
      title="Approval of confirmed changes"
      description="Whether a destructive change the user confirmed when submitting it still waits here."
    >
      {state.status === 'loading' && <LoadingState rows={1} />}
      {state.status === 'error' && (
        <Alert status="error" title="The Change Plan policy could not be loaded">
          <Stack gap="2" align="start">
            <FailureMessage failure={state} />
            <Button size="sm" variant="outline" onClick={retry}>Try again</Button>
          </Stack>
        </Alert>
      )}
      {state.status === 'ready' && (
        <Stack gap="3">
          <Switch.Root
            checked={!state.policy.approveOnSubmit}
            disabled={saving}
            colorPalette="red"
            onCheckedChange={(details) => void setRequireApproval(details.checked === true)}
          >
            <Switch.HiddenInput role="switch" aria-describedby={descriptionId} />
            <Switch.Control><Switch.Thumb /></Switch.Control>
            <Switch.Label fontWeight="medium">Require a separate approval for every Change Plan</Switch.Label>
          </Switch.Root>
          <Text id={descriptionId} fontSize="sm" color="fg.muted">
            {state.policy.approveOnSubmit
              ? 'Off: a change confirmed in the dashboard, or sent with the CLI’s --approve flag, is approved when it is submitted and starts at once. Plans submitted without approval, plans with blockers, and plans Swallow proposes on its own still wait here.'
              : 'On: every destructive change waits here until an admin approves it, even one the user confirmed when submitting it. Turning it off affects only changes submitted afterwards.'}
          </Text>
          <Text fontSize="sm" color="fg.muted">
            {state.policy.updatedAt
              ? `Last changed by ${state.policy.updatedBy || 'an admin'} at ${formatDateTime(state.policy.updatedAt)}.`
              : 'Never changed; the default lets confirmed changes start at once.'}
          </Text>
          {saveError && (
            <Alert status="error" title="The policy was not changed">
              <FailureMessage failure={saveError} />
            </Alert>
          )}
        </Stack>
      )}
    </SectionSurface>
  )
}
