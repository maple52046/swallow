import { Alert as ChakraAlert } from '@chakra-ui/react'
import type { ReactNode } from 'react'

export interface AlertProps extends Omit<ChakraAlert.RootProps, 'title'> {
  /** Short headline; the primary message an operator scans first. */
  title?: ReactNode
  /** Supporting detail rendered as the alert description. */
  children?: ReactNode
  /** Optional trailing controls (e.g. a retry or dismiss action). */
  actions?: ReactNode
}

/**
 * Shared inline alert wrapping Chakra's compound `Alert`.
 *
 * Used for banner-style, in-context messaging (validation summaries, degraded
 * states, informational notes). Pass `status` for tone (`info` | `success` |
 * `warning` | `error`); the status indicator plus text carry meaning, so tone is
 * never conveyed by colour alone. Transient action feedback uses the toast channel
 * instead of this component.
 */
export function Alert({ title, children, actions, ...rest }: AlertProps) {
  return (
    <ChakraAlert.Root {...rest}>
      <ChakraAlert.Indicator />
      <ChakraAlert.Content>
        {title && <ChakraAlert.Title>{title}</ChakraAlert.Title>}
        {children && <ChakraAlert.Description>{children}</ChakraAlert.Description>}
      </ChakraAlert.Content>
      {actions}
    </ChakraAlert.Root>
  )
}
