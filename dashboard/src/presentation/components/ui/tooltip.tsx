import { Tooltip as ChakraTooltip, Portal } from '@chakra-ui/react'
import { forwardRef, type ReactNode } from 'react'

export interface TooltipProps extends ChakraTooltip.RootProps {
  /** Tooltip body; keep it short — it must stay readable on hover/focus. */
  content: ReactNode
  showArrow?: boolean
  /** When true, renders children unchanged with no tooltip (e.g. disabled triggers). */
  disabled?: boolean
  portalled?: boolean
  contentProps?: ChakraTooltip.ContentProps
  children: ReactNode
}

/**
 * Thin wrapper over Chakra's compound `Tooltip` giving a `content`-prop API.
 *
 * Used across the console for icon-only controls (collapsed nav, copy buttons,
 * status glyphs) where the visible affordance needs an accessible name on hover
 * and keyboard focus. The trigger must be a single focusable element; the tooltip
 * is supplementary and never the only place a label appears for assistive tech.
 */
export const Tooltip = forwardRef<HTMLDivElement, TooltipProps>(function Tooltip(props, ref) {
  const { content, showArrow, disabled, portalled = true, contentProps, children, ...rest } = props
  if (disabled) return <>{children}</>
  return (
    <ChakraTooltip.Root openDelay={200} closeDelay={80} {...rest}>
      <ChakraTooltip.Trigger asChild>{children}</ChakraTooltip.Trigger>
      <Portal disabled={!portalled}>
        <ChakraTooltip.Positioner>
          <ChakraTooltip.Content ref={ref} {...contentProps}>
            {showArrow && (
              <ChakraTooltip.Arrow>
                <ChakraTooltip.ArrowTip />
              </ChakraTooltip.Arrow>
            )}
            {content}
          </ChakraTooltip.Content>
        </ChakraTooltip.Positioner>
      </Portal>
    </ChakraTooltip.Root>
  )
})
