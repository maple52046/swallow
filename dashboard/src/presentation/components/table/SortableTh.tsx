import { Table, Flex, Text } from '@radix-ui/themes'
import { ArrowDownIcon, ArrowUpIcon } from '@radix-ui/react-icons'
import type { SortDirection } from '@/domain/server/list'

interface SortableThProps {
  label: string
  /** Optional muted second label for a double header (e.g. "FQDN" over "MAC"). */
  secondary?: string
  /** This column's sort key; compared against `activeKey` to show the indicator. */
  sortKey: string
  activeKey: string | null
  direction: SortDirection
  onSort: (key: string) => void
  align?: 'start' | 'end'
}

/**
 * A sortable column header, replacing MAAS's sortable `TableHeader`.
 *
 * Clicking cycles this column through the caller's sort model (typically desc → asc →
 * off); the active direction shows as an arrow, and `aria-sort` exposes it to assistive
 * tech so sort state is not conveyed by the icon alone. The header is a real button for
 * keyboard use.
 */
export function SortableTh({
  label,
  secondary,
  sortKey,
  activeKey,
  direction,
  onSort,
  align = 'start',
}: SortableThProps) {
  const isActive = activeKey === sortKey
  const ariaSort = isActive ? (direction === 'asc' ? 'ascending' : 'descending') : 'none'

  return (
    <Table.ColumnHeaderCell aria-sort={ariaSort}>
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        style={{
          background: 'transparent',
          border: 'none',
          padding: 0,
          cursor: 'pointer',
          width: '100%',
          color: 'inherit',
          font: 'inherit',
        }}
      >
        <Flex align="center" gap="1" justify={align === 'end' ? 'end' : 'start'}>
          <Flex direction="column" align={align}>
            <Text size="2" weight="medium">
              {label}
            </Text>
            {secondary && (
              <Text size="1" color="gray">
                {secondary}
              </Text>
            )}
          </Flex>
          {isActive && (direction === 'asc' ? <ArrowUpIcon /> : <ArrowDownIcon />)}
        </Flex>
      </button>
    </Table.ColumnHeaderCell>
  )
}
