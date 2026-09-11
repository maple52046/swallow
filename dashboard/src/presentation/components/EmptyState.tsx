import { Button, EmptyState as ChakraEmptyState, VStack } from '@chakra-ui/react'
import { Inbox } from 'lucide-react'
import type { ReactNode } from 'react'

interface EmptyStateProps {
  title?: string
  message?: string
  /** Optional custom indicator; defaults to a neutral inbox glyph. */
  icon?: ReactNode
  /** Optional primary recovery action (e.g. clear filters, create the first record). */
  action?: { label: string; onClick: () => void }
}

/**
 * Shared empty result, visually distinct from loading and failure.
 *
 * Rendered when a query succeeds but returns nothing. The optional `action` gives
 * the operator a recovery path (clear filters, create the first record) so an
 * empty surface never feels like a dead end.
 */
export function EmptyState({ title = 'No data', message, icon, action }: EmptyStateProps) {
  return (
    <ChakraEmptyState.Root>
      <ChakraEmptyState.Content>
        <ChakraEmptyState.Indicator>{icon ?? <Inbox />}</ChakraEmptyState.Indicator>
        <VStack textAlign="center" gap="1">
          <ChakraEmptyState.Title>{title}</ChakraEmptyState.Title>
          {message && <ChakraEmptyState.Description>{message}</ChakraEmptyState.Description>}
        </VStack>
        {action && (
          <Button colorPalette="brand" onClick={action.onClick} mt="2">
            {action.label}
          </Button>
        )}
      </ChakraEmptyState.Content>
    </ChakraEmptyState.Root>
  )
}
