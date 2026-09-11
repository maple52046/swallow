import { chakra, Grid, Stack } from '@chakra-ui/react'
import type { ReactNode } from 'react'

export interface DescriptionItem {
  label: string
  value: ReactNode
}

interface DescriptionListProps {
  items: DescriptionItem[]
  /** Placeholder shown when a value is empty/absent, so a blank never reads as zero. */
  emptyText?: string
}

/**
 * Shared horizontal term/description list for compact metadata blocks (server
 * summary, provider details, BMC sections). Terms sit in a muted left column and
 * wrap to stacked rows on narrow viewports. A falsy value renders `emptyText`
 * rather than an empty cell.
 */
export function DescriptionList({ items, emptyText = 'Not observed' }: DescriptionListProps) {
  return (
    <Stack as="dl" gap="2.5" m="0">
      {items.map((item) => (
        <Grid
          key={item.label}
          templateColumns={{ base: '1fr', sm: 'minmax(8rem, 12rem) 1fr' }}
          columnGap="4"
          rowGap="0.5"
        >
          <chakra.dt color="fg.muted" fontSize="sm">
            {item.label}
          </chakra.dt>
          <chakra.dd m="0" minW="0" css={{ overflowWrap: 'anywhere' }}>
            {item.value || emptyText}
          </chakra.dd>
        </Grid>
      ))}
    </Stack>
  )
}
