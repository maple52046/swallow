import { Box, HStack, List, Progress, Stack, Text, VisuallyHidden } from '@chakra-ui/react'
import { CheckCircle2, Circle } from 'lucide-react'
import type { BootMediaApply } from '@/domain/server/types'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { useNow } from '@/presentation/hooks/useNow'
import { formatDuration } from '@/shared/utils/time'
import { bootMediaApplyProgress } from './bootMediaApplyProgress'

interface BootMediaApplyProgressProps {
  /** The API's record of the running preflight; `null` until the first poll answers. */
  apply: BootMediaApply | null
  /** When this client sent the request (epoch ms), the start until the API reports its own. */
  requestedAt?: number
}

/**
 * The progress of a Boot Media enable preflight (decisions 047 and 049), shared by the enable
 * dialog and the Server Summary's Boot media block so both read the same while it runs.
 *
 * It shows a bar, the steps (done, current, ahead), the time since it started, and — during the
 * settle wait, the long part — the time left in it. The phase comes from the API (`apply`, polled
 * by the caller); the bar is weighted by each step's typical duration and only the settle wait
 * has a known end, so the bar is an estimate that never claims completion. The note says the
 * wait is expected, so a long, quiet step does not read as a hang.
 *
 * Accessibility: the bar is a labelled progressbar whose value text is the current step; the
 * step list marks the current step with `aria-current`; status is in words, icons are decorative.
 * The clock is not a live region (it would announce every second); the step change is, politely.
 */
export function BootMediaApplyProgress({ apply, requestedAt }: BootMediaApplyProgressProps) {
  const now = useNow(1_000)
  const progress = bootMediaApplyProgress(apply, requestedAt ?? now, now)
  return (
    <Stack gap="3">
      <Stack gap="1">
        <HStack justify="space-between" fontSize="sm" gap="3" wrap="wrap">
          <Text fontWeight="medium" aria-live="polite">
            {progress.summary}
          </Text>
          <Text as="span" color="fg.muted" fontVariantNumeric="tabular-nums">
            <VisuallyHidden>Running for </VisuallyHidden>
            {formatDuration(Math.max(progress.elapsedMs, 1_000))}
            {progress.settleLeftMs !== null && ` · ${formatDuration(Math.max(progress.settleLeftMs, 1_000))} left in this step`}
          </Text>
        </HStack>
        <Progress.Root value={progress.percent} size="sm" colorPalette="brand">
          {/* The track is the element with role progressbar; its default name is only the percentage. */}
          <Progress.Track aria-label="Boot Media progress" aria-valuetext={`${progress.percent}% — ${progress.summary}`}>
            <Progress.Range />
          </Progress.Track>
        </Progress.Root>
      </Stack>
      <List.Root as="ol" listStyleType="none" gap="1.5" fontSize="sm">
        {progress.steps.map((step) => (
          <List.Item key={step.key} aria-current={step.status === 'current' ? 'step' : undefined}>
            <HStack gap="2" align="center">
              <Box as="span" display="inline-flex" w="4" justifyContent="center" color={step.status === 'done' ? 'green.fg' : 'fg.muted'}>
                {step.status === 'done' && <CheckCircle2 size={16} aria-hidden />}
                {step.status === 'current' && <InProgressSpinner color="brand.solid" />}
                {step.status === 'pending' && <Circle size={14} aria-hidden />}
              </Box>
              <Text as="span" fontWeight={step.status === 'current' ? 'medium' : undefined} color={step.status === 'pending' ? 'fg.muted' : undefined}>
                {step.label}
                <VisuallyHidden>{step.status === 'done' ? ' (done)' : step.status === 'current' ? ' (in progress)' : ' (not started)'}</VisuallyHidden>
              </Text>
            </HStack>
          </List.Item>
        ))}
      </List.Root>
      <Text color="fg.muted" fontSize="xs">
        This usually takes about five minutes, most of it the three-minute wait a freshly mounted ISO needs before the host
        may power on. Nothing is rebooted.
      </Text>
    </Stack>
  )
}
