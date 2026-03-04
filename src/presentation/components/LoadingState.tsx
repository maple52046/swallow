import { Stack, Skeleton, type StackProps } from '@mantine/core'

interface LoadingStateProps extends StackProps {
  rows?: number
  height?: number
}

export function LoadingState({ rows = 5, height = 40, ...rest }: LoadingStateProps) {
  return (
    <Stack gap="sm" {...rest}>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} height={height} radius="sm" />
      ))}
    </Stack>
  )
}
