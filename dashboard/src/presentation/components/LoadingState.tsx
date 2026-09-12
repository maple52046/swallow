import { Skeleton, Stack } from '@chakra-ui/react'

interface LoadingStateProps {
  rows?: number
  /** Row height in pixels; matches the geometry of the content being awaited. */
  height?: number
}

/**
 * Shared loading placeholder for route and section data.
 *
 * Skeleton rows preserve geometry while the live region announces one busy state;
 * decorative rows are never read individually by assistive technology.
 */
export function LoadingState({ rows = 5, height = 40 }: LoadingStateProps) {
  return (
    <Stack
      gap="3"
      p={{ base: '4', md: '5' }}
      rounded="xl"
      borderWidth="1px"
      borderColor="border"
      bg="bg.panel"
      aria-busy="true"
      aria-live="polite"
      aria-label="Loading content"
    >
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} height={`${height}px`} rounded="lg" />
      ))}
    </Stack>
  )
}
