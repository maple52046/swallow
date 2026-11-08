import { useId, useState } from 'react'
import { Button, HStack, Link as ChakraLink, Stack, Switch, Text } from '@chakra-ui/react'
import { Link, useNavigate } from 'react-router-dom'
import { changePlanWaitDescription } from '@/presentation/components/changePlanWait'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { SectionHeader } from '@/presentation/components/ConsolePrimitives'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import type { PlatformSectionProps } from '@/modules/types'
import { useSlurmPlatform } from './SlurmModuleProvider'
import { hardwareCountersSwitch } from './slurmHardwareCounters'

/**
 * Omnistat's GPU hardware counters (glossary Omnistat Hardware Counters) on a deployed Slurm
 * Platform's Configuration route: whether the spec asks for them, what the last Omnistat install did
 * with that, and a switch that turns them on or off (platforms.md "Omnistat").
 *
 * The switch shows the spec's setting. Turning it asks for confirmation that names the consequences
 * (each compute node's exporter restarts, running jobs keep running), then writes the spec through
 * the Slurm port; the change is non-destructive, so it starts without a Change Plan and the page
 * follows it through the workspace shell's convergence polling. The switch is disabled, with the
 * reason as text, while the Platform is not active or a change is still running. The request's
 * error (a `conflict` when the spec changed since it was read) stays in the dialog without reloading
 * the page, which would close it; confirming again writes the spec the port reads anew, so another
 * write is never overwritten. Each compute node's own outcome is on the Monitoring route.
 * Renders nothing for a Slurm Platform without a swallow-managed Omnistat.
 */
export function SlurmHardwareCountersSection({ platform, onChanged, onOpenWorkflow }: PlatformSectionProps) {
  const slurm = useSlurmPlatform()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const descriptionId = useId()
  // The setting the open confirmation turns the counters to; kept after it closes so its title does
  // not change while the dialog animates out.
  const [target, setTarget] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const counters = hardwareCountersSwitch(platform)
  if (!counters) return null
  const workflowId = platform.omnistat?.lastWorkflowId ?? null

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
        description: 'Omnistat is reconfigured on every compute node; the Platform shows the outcome when the workflow finishes.',
      })
      onChanged()
    } catch (caught) {
      setError(failureOf(caught, 'Could not change the hardware counters.'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="sw-section" aria-label="GPU hardware counters">
      <SectionHeader
        title="GPU hardware counters"
        description="Omnistat samples VALUBusy, how busy each GPU's vector ALUs are, on every compute node that can."
      />
      <Stack gap="3" className="sw-section-content">
        <Switch.Root
          checked={counters.wanted}
          colorPalette="brand"
          disabled={Boolean(counters.blockedReason) || submitting}
          onCheckedChange={(details) => {
            setError(null)
            setTarget(details.checked === true)
            setConfirming(true)
          }}
        >
          <Switch.HiddenInput role="switch" aria-describedby={descriptionId} />
          <Switch.Control><Switch.Thumb /></Switch.Control>
          <Switch.Label fontWeight="medium">Collect GPU hardware counters (VALUBusy)</Switch.Label>
        </Switch.Root>
        <Stack gap="2" id={descriptionId}>
          <HStack gap="2" flexWrap="wrap">
            <StatusBadge status={counters.status.value} label={counters.status.label} />
            {counters.inProgress && <InProgressSpinner color="blue.fg" />}
            {workflowId && (
              <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={() => onOpenWorkflow(workflowId)}>
                View workflow
              </Button>
            )}
          </HStack>
          <Text fontSize="sm">{counters.summary}</Text>
          {counters.blockedReason && <Text color="fg.muted" fontSize="sm">{counters.blockedReason}</Text>}
          <Text color="fg.muted" fontSize="sm">
            Each compute node's outcome, and why a node does not collect them, is on{' '}
            <ChakraLink asChild color="brand.fg" textDecoration="underline" textUnderlineOffset="3px">
              <Link to={scopedHref(`/platforms/${encodeURIComponent(platform.id)}/monitoring`, platform.siteId)}>Monitoring</Link>
            </ChakraLink>.
          </Text>
        </Stack>
      </Stack>

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
              ? `Swallow reconfigures Omnistat on every compute node of ${platform.name}. A node that can collect VALUBusy gets the counter extension, CAP_PERFMON, and a profiler session that holds about 160 MB of GPU memory per GPU; a node that cannot keeps its other metrics and reports why.`
              : `Swallow takes every compute node of ${platform.name} back to Omnistat without the counters: the counter extension Swallow installed, CAP_PERFMON, and the profiler session are removed, and VALUBusy is no longer collected. Omnistat's other metrics continue.`}
          </Text>
          <Text>Each node's exporter restarts with its new configuration; running jobs keep running.</Text>
          {error && <Alert status="error" title="The change could not be started"><FailureMessage failure={error} /></Alert>}
        </Stack>
      </ConfirmDialog>
    </section>
  )
}
