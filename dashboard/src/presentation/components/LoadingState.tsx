import { Skeleton, Stack } from '@chakra-ui/react'

interface LoadingStateProps {
  rows?: number
  /** Row height in pixels; matches the geometry of the content being awaited. */
  height?: number
}

/**
 * Shared loading placeholder.
 *
 * Skeleton rows preserve page geometry while remote data resolves, and the
 * container announces a busy state (`aria-busy` + polite live region) without
 * assistive tech reading each decorative row.
 */
export function LoadingState({ rows = 5, height = 40 }: LoadingStateProps) {
  return (
    <Stack gap="2" aria-busy="true" aria-live="polite" aria-label="Loading content">
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} height={`${height}px`} rounded="md" />
      ))}
    </Stack>
  )
}
