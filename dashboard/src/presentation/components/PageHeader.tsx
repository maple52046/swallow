import { Flex, Heading, Text } from '@radix-ui/themes'
import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  subtitle?: string
  /** Right-aligned actions, e.g. a status badge or buttons. */
  actions?: ReactNode
}

/**
 * The shared page title block, used at the top of every routed screen.
 *
 * Keeps title, optional subtitle, and optional right-aligned actions consistent across
 * pages. Reused rather than hand-written per page (coding-style DRY gate).
 */
export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <Flex justify="between" align="start" mb="4" gap="3" wrap="wrap">
      <Flex direction="column" gap="1">
        <Heading as="h1" size="6">
          {title}
        </Heading>
        {subtitle && (
          <Text color="gray" size="2">
            {subtitle}
          </Text>
        )}
      </Flex>
      {actions && (
        <Flex gap="2" align="center">
          {actions}
        </Flex>
      )}
    </Flex>
  )
}
