import { useState } from 'react'
import { HStack, Stack, Switch, Text } from '@chakra-ui/react'
import { useNavigate } from 'react-router-dom'
import { changePlanWaitDescription } from '@/presentation/components/changePlanWait'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import type { PlatformSectionProps } from '@/modules/types'
import { useSlurmPlatform } from './SlurmModuleProvider'
import { COLLECTED_HARDWARE_COUNTERS, hardwareCountersSwitch } from './slurmHardwareCounters'

/**
 * The switch for Omnistat's GPU hardware counters (glossary Omnistat Hardware Counters) on a deployed
 * Slurm Platform, rendered by the Monitoring route right under the counters' title in the Omnistat
 * component, above the summary and each compute node's outcome (platforms.md "Omnistat").
 *
 * Turning the counters on enables Omnistat's counter collection; which counters it samples is a
 * separate, fixed list (`COLLECTED_HARDWARE_COUNTERS`), named under the switch. The switch shows the
 * spec's setting and the badge what the last Omnistat install did with it. Turning it asks for
 * confirmation that names what changes on the nodes, how long it takes, and what users notice (Slurm
 * and running jobs untouched, each exporter restarting once, and while the counters are on a user's
 * own GPU profiler possibly not getting them), then writes the spec through the Slurm port; the change is
 * non-destructive, so it starts without a Change Plan and the page follows it through the workspace
 * shell's convergence polling. The switch is disabled, with the reason as text, while the Platform
 * is not active, a change is still running, or the user may not change the Platform. The request's error (a `conflict` when the spec
 * changed since it was read) stays in the dialog without reloading the page, which would close it;
 * confirming again writes the spec the port reads anew, so another write is never overwritten.
 * Renders nothing for a Slurm Platform without a swallow-managed Omnistat.
 */
export function SlurmHardwareCountersControl({ platform, canOperate, onChanged }: PlatformSectionProps) {
  const slurm = useSlurmPlatform()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  // The setting the open confirmation turns the counters to; kept after it closes so its title does
  // not change while the dialog animates out.
  const [target, setTarget] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const counters = hardwareCountersSwitch(platform)
  if (!counters) return null
  const blockedReason = canOperate ? counters.blockedReason : 'Only an owner of the Platform’s Pool or an admin can change this.'

  const close = () => {
    if (submitting) return
    setConfirming(false)
    setError(null)
  }
  const submit = async (wanted: boolean) => {
    setSubmitting(true)
    setError(null)
    try {
      const written = await slurm.setHardwareCounters(platform.id, wanted)
      setConfirming(false)
      if (written.awaitingApproval) {
        showToast({ tone: 'warning', title: 'The hardware counters change waits for approval', description: changePlanWaitDescription(written, 'apply it') })
        navigate(scopedHref('/change-plans'))
        return
      }
      showToast({
        tone: 'success',
        title: wanted ? 'Turning on the hardware counters' : 'Turning off the hardware counters',
        description: 'Omnistat is reconfigured on every compute node; each node\u2019s outcome appears here when the workflow finishes.',
      })
      onChanged()
    } catch (caught) {
      setError(failureOf(caught, 'Could not change the hardware counters.'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Stack gap="1" py="1">
      <HStack gap="3" flexWrap="wrap">
        <Switch.Root
          checked={counters.wanted}
          colorPalette="brand"
          disabled={Boolean(blockedReason) || submitting}
          onCheckedChange={(details) => {
            setError(null)
            setTarget(details.checked === true)
            setConfirming(true)
          }}
        >
          <Switch.HiddenInput role="switch" />
          <Switch.Control><Switch.Thumb /></Switch.Control>
          <Switch.Label fontSize="sm">Collect GPU hardware counters</Switch.Label>
        </Switch.Root>
        <HStack gap="2">
          <StatusBadge status={counters.status.value} label={counters.status.label} />
          {counters.inProgress && <InProgressSpinner color="blue.fg" />}
        </HStack>
      </HStack>
      <Text color="fg.muted" fontSize="sm">Counters collected: {COLLECTED_HARDWARE_COUNTERS}.</Text>
      {blockedReason && <Text color="fg.muted" fontSize="sm">{blockedReason}</Text>}

      <ConfirmDialog
        open={confirming}
        title={target ? 'Turn on GPU hardware counters' : 'Turn off GPU hardware counters'}
        confirmLabel={target ? 'Turn on counters' : 'Turn off counters'}
        destructive={false}
        busy={submitting}
        onCancel={close}
        onConfirm={() => void submit(target)}
      >
        <Stack gap="3">
          <Text>
            {target
              ? `Swallow turns on Omnistat's GPU hardware counters on every compute node of ${platform.name}. A node that can collect them gets the counter extension and CAP_PERFMON, and its exporter keeps a profiler session on each GPU (about 160 MB of GPU memory per GPU) to sample ${COLLECTED_HARDWARE_COUNTERS}. A node that cannot keeps its other metrics and reports why.`
              : `Swallow takes every compute node of ${platform.name} back to Omnistat without hardware counters: the counter extension Swallow installed, CAP_PERFMON, and the profiler sessions are removed, and the counters are no longer collected. Omnistat's other metrics continue.`}
          </Text>
          {target && (
            <Text>While the counters are on, a user's own GPU profiler on these nodes (rocprofv3, Omniperf) may not get the counters, because a GPU allows one profiler session at a time.</Text>
          )}
          <Text>It usually takes a few minutes. Slurm and running jobs are not touched; each node's Omnistat exporter restarts once near the end, so its metrics can miss a scrape.</Text>
          {error && <Alert status="error" title="The change could not be started"><FailureMessage failure={error} /></Alert>}
        </Stack>
      </ConfirmDialog>
    </Stack>
  )
}
