import { Skeleton } from '@patternfly/react-core'

interface LoadingStateProps {
  rows?: number
  height?: number
}

/**
 * Shared loading placeholder. Skeleton rows preserve page geometry and the container
 * announces busy state without repeatedly speaking each decorative row.
 */
export function LoadingState({ rows = 5, height = 40 }: LoadingStateProps) {
  return (
    <div className="sw-loading" aria-busy="true" aria-live="polite" aria-label="Loading content">
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} height={`${height}px`} screenreaderText={index === 0 ? 'Loading' : undefined} />
      ))}
    </div>
  )
}
