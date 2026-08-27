import { Pagination as PatternFlyPagination, PaginationVariant } from '@patternfly/react-core'

interface PaginationProps {
  total: number
  value: number
  onChange: (page: number) => void
}

/** Compact PatternFly page navigator; total and value are both page counts, not rows. */
export function Pagination({ total, value, onChange }: PaginationProps) {
  if (total <= 1) return null
  return (
    <PatternFlyPagination
      itemCount={total}
      perPage={1}
      page={value}
      variant={PaginationVariant.bottom}
      isCompact
      onSetPage={(_event, page) => onChange(page)}
      titles={{ paginationAriaLabel: 'Pagination' }}
    />
  )
}
