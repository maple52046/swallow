import { IconButton, Flex, Text } from '@radix-ui/themes'
import { ChevronLeftIcon, ChevronRightIcon } from '@radix-ui/react-icons'

interface PaginationProps {
  /** Total number of pages (>= 1). */
  total: number
  /** Current 1-based page. */
  value: number
  onChange: (page: number) => void
}

/**
 * Page navigator, since Radix Themes ships no pagination.
 *
 * Shows previous/next plus a small window of numbered pages around the current one, so a
 * large fleet does not render hundreds of buttons. Buttons carry `aria-label`s and the
 * current page is marked `aria-current` for assistive tech; the active page is not
 * indicated by colour alone.
 */
export function Pagination({ total, value, onChange }: PaginationProps) {
  if (total <= 1) return null

  const pages = windowedPages(value, total)

  return (
    <Flex gap="1" align="center" justify="center" aria-label="Pagination">
      <IconButton
        variant="soft"
        color="gray"
        aria-label="Previous page"
        disabled={value <= 1}
        onClick={() => onChange(value - 1)}
      >
        <ChevronLeftIcon />
      </IconButton>

      {pages.map((page, index) =>
        page === ELLIPSIS ? (
          <Text key={`gap-${index}`} size="2" color="gray" mx="1">
            …
          </Text>
        ) : (
          <IconButton
            key={page}
            variant={page === value ? 'solid' : 'soft'}
            color={page === value ? undefined : 'gray'}
            aria-label={`Page ${page}`}
            aria-current={page === value ? 'page' : undefined}
            onClick={() => onChange(page)}
          >
            {page}
          </IconButton>
        ),
      )}

      <IconButton
        variant="soft"
        color="gray"
        aria-label="Next page"
        disabled={value >= total}
        onClick={() => onChange(value + 1)}
      >
        <ChevronRightIcon />
      </IconButton>
    </Flex>
  )
}

/** Sentinel for a collapsed run of pages. */
const ELLIPSIS = -1

/**
 * Builds the page list to render: first, last, and a window around the current page, with
 * ellipsis sentinels standing in for the collapsed runs.
 */
function windowedPages(current: number, total: number): number[] {
  const window = 1
  const pages = new Set<number>([1, total])
  for (let page = current - window; page <= current + window; page++) {
    if (page >= 1 && page <= total) pages.add(page)
  }

  const sorted = [...pages].sort((a, b) => a - b)
  const result: number[] = []
  let previous = 0
  for (const page of sorted) {
    if (previous && page - previous > 1) result.push(ELLIPSIS)
    result.push(page)
    previous = page
  }
  return result
}
