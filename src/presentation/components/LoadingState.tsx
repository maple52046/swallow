import { Flex, Skeleton } from '@radix-ui/themes'

interface LoadingStateProps {
  /** Number of skeleton rows to show. Roughly match the content it stands in for. */
  rows?: number
  /** Height of each row in pixels. */
  height?: number
}

/**
 * The shared loading placeholder used by every data screen.
 *
 * Renders a stack of skeleton rows rather than a spinner so the page keeps its shape
 * while data loads. Always prefer this over a bespoke loader (coding-style DRY gate).
 */
export function LoadingState({ rows = 5, height = 40 }: LoadingStateProps) {
  return (
    <Flex direction="column" gap="2" aria-busy="true" aria-live="polite">
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} height={`${height}px`} />
      ))}
    </Flex>
  )
}
