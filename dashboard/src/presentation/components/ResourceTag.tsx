import { Badge } from '@chakra-ui/react'
import type { ReactNode } from 'react'

interface ResourceTagProps {
  children: ReactNode
  /** Optional accessible name for compact summaries such as a hidden-tag count. */
  ariaLabel?: string
}

/**
 * Presents an operator-authored resource Tag consistently across inventory,
 * detail, and editing surfaces.
 *
 * The blue treatment identifies user organization metadata, not lifecycle or
 * health. Interactive children, such as a remove button, keep responsibility
 * for their own accessible name and keyboard behavior.
 */
export function ResourceTag({ children, ariaLabel }: ResourceTagProps) {
  return (
    <Badge
      className="sw-resource-tag"
      colorPalette="blue"
      variant="subtle"
      aria-label={ariaLabel}
    >
      {children}
    </Badge>
  )
}

