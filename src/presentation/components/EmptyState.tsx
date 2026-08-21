import { Button, Flex, Text } from '@radix-ui/themes'
import { ArchiveIcon } from '@radix-ui/react-icons'
import type { ReactNode } from 'react'

interface EmptyStateProps {
  title?: string
  message?: string
  /** Optional custom icon; defaults to an archive glyph. */
  icon?: ReactNode
  /** Optional primary action, e.g. "Clear filters". */
  action?: { label: string; onClick: () => void }
}

/**
 * The shared empty-result surface for lists and tables.
 *
 * Distinguishes "nothing here" from loading and error, which is why it is its own
 * component and always reused (coding-style DRY gate) rather than an inline message. The
 * message should say why it is empty (no data vs. no match for filters) so an operator
 * can act.
 */
export function EmptyState({ title = 'No data', message, icon, action }: EmptyStateProps) {
  return (
    <Flex direction="column" align="center" gap="2" py="6">
      <Text color="gray" size="5">
        {icon ?? <ArchiveIcon width="28" height="28" />}
      </Text>
      <Text weight="medium" color="gray">
        {title}
      </Text>
      {message && (
        <Text size="2" color="gray" align="center" style={{ maxWidth: 360 }}>
          {message}
        </Text>
      )}
      {action && (
        <Button variant="soft" size="2" onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </Flex>
  )
}
