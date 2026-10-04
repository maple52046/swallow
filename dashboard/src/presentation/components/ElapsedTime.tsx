import { Box, VisuallyHidden } from '@chakra-ui/react'
import { Timer } from 'lucide-react'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useNow } from '@/presentation/hooks/useNow'
import { formatDuration } from '@/shared/utils/time'

/**
 * A running time that ticks every second from `since` (an ISO timestamp from the API), shown
 * under or beside an in-progress Deployment state. `description` explains what the start time
 * means, because a Swallow deployment's start and a provider state's first observation differ
 * in precision; it is the hover text. Renders nothing for an unparsable time.
 *
 * The visible value is not a live region: announcing it every second would drown out the page.
 * Assistive technology reads it on demand with its "Running for" prefix. A browser clock behind
 * the server's never shows a negative time.
 */
export function ElapsedTime({ since, description }: { since: string; description: string }) {
  const now = useNow(1_000)
  const start = Date.parse(since)
  if (Number.isNaN(start)) return null
  const elapsed = Math.max(0, now - start)
  return (
    <Tooltip content={description}>
      <Box
        as="span"
        className="sw-elapsed-time"
        display="inline-flex"
        alignItems="center"
        gap="1"
        color="fg.muted"
        fontSize="xs"
        whiteSpace="nowrap"
        fontVariantNumeric="tabular-nums"
      >
        <Timer aria-hidden />
        <VisuallyHidden>Running for </VisuallyHidden>
        {elapsed < 1_000 ? '0s' : formatDuration(elapsed)}
      </Box>
    </Tooltip>
  )
}
