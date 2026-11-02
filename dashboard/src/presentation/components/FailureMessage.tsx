import { Stack, Text } from '@chakra-ui/react'
import type { DisplayedFailure } from '@/presentation/components/failure'

interface FailureMessageProps {
  /** A failure from `failureOf`, or a client-side message that has no request behind it. */
  failure: DisplayedFailure | string
}

/**
 * The shared body of an error message: the user-facing text and, below it, the request ID an
 * user quotes when reporting the failure (dashboard architecture spec, API and Error Boundary).
 * Place it inside an error `Alert`, a form error, or any other error container.
 */
export function FailureMessage({ failure }: FailureMessageProps) {
  const { message, requestId } = typeof failure === 'string' ? { message: failure, requestId: undefined } : failure
  if (!requestId) return <>{message}</>
  return (
    <Stack as="span" gap="0.5" display="inline-flex">
      <span>{message}</span>
      <Text as="span" fontSize="xs" color="fg.muted" className="mono">
        Request ID: {requestId}
      </Text>
    </Stack>
  )
}
