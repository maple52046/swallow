import { Button, EmptyState as ChakraEmptyState, VStack } from '@chakra-ui/react'
import { Inbox } from 'lucide-react'
import type { ReactNode } from 'react'

interface EmptyStateProps {
  title?: string
  message?: string
  /** Optional custom indicator; defaults to a neutral inbox glyph. */
  icon?: ReactNode
  /** Optional primary recovery action, such as clearing filters or creating a resource. */
  action?: { label: string; onClick: () => void }
}

/**
 * Shared successful-but-empty state.
 *
 * Copy names the empty result; the optional action provides one direct recovery
 * path. The bounded surface keeps it visually distinct from loading and failure.
 */
export function EmptyState({ title = 'No data', message, icon, action }: EmptyStateProps) {
  return (
    <ChakraEmptyState.Root minH="15rem" rounded="xl" borderWidth="1px" borderColor="border" bg="bg.panel">
      <ChakraEmptyState.Content>
        <ChakraEmptyState.Indicator rounded="xl" bg="brand.subtle" color="brand.fg">
          {icon ?? <Inbox />}
        </ChakraEmptyState.Indicator>
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
