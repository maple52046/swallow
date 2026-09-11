import { ButtonGroup, Flex, IconButton, Pagination as ChakraPagination } from '@chakra-ui/react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

interface PaginationProps {
  /** Total number of pages (not rows). */
  total: number
  /** The current 1-based page. */
  value: number
  onChange: (page: number) => void
}

/**
 * Compact page navigator, right-aligned beneath a paged list.
 *
 * `total` and `value` are page counts, not row counts — the underlying Chakra
 * pagination is driven with a page size of one so one "item" equals one page.
 * Renders nothing for a single page so short lists stay uncluttered.
 */
export function Pagination({ total, value, onChange }: PaginationProps) {
  if (total <= 1) return null
  return (
    <Flex justify="flex-end">
      <ChakraPagination.Root
        count={total}
        pageSize={1}
        page={value}
        onPageChange={(details) => onChange(details.page)}
      >
        <ButtonGroup variant="ghost" size="sm" gap="1">
          <ChakraPagination.PrevTrigger asChild>
            <IconButton aria-label="Previous page">
              <ChevronLeft size={16} />
            </IconButton>
          </ChakraPagination.PrevTrigger>
          <ChakraPagination.PageText />
          <ChakraPagination.NextTrigger asChild>
            <IconButton aria-label="Next page">
              <ChevronRight size={16} />
            </IconButton>
          </ChakraPagination.NextTrigger>
        </ButtonGroup>
      </ChakraPagination.Root>
    </Flex>
  )
}
