import { Table, Flex, IconButton, Text } from '@radix-ui/themes'
import { ChevronDownIcon, ChevronRightIcon } from '@radix-ui/react-icons'
import type { ReactNode } from 'react'

interface GroupHeaderRowProps {
  /** Number of columns to span, so the header stretches the whole table width. */
  colSpan: number
  label: string
  count: number
  collapsed: boolean
  onToggleCollapse: () => void
  /** The group selection checkbox, supplied by the page which owns selection state. */
  checkbox: ReactNode
}

/**
 * A grouped-table section header, replacing MAAS's group row.
 *
 * Spans every column and carries the group's selection checkbox, its name, a member
 * count, and a show/hide toggle. The toggle is a real button whose `aria-expanded`
 * reflects collapse state, so the section can be operated by keyboard and its state is
 * announced.
 */
export function GroupHeaderRow({
  colSpan,
  label,
  count,
  collapsed,
  onToggleCollapse,
  checkbox,
}: GroupHeaderRowProps) {
  return (
    <Table.Row>
      <Table.Cell colSpan={colSpan} style={{ background: 'var(--gray-a2)' }}>
        <Flex align="center" justify="between">
          <Flex align="center" gap="2">
            {checkbox}
            <Text size="2" weight="bold">
              {label}
            </Text>
            <Text size="1" color="gray">
              {count} {count === 1 ? 'server' : 'servers'}
            </Text>
          </Flex>
          <IconButton
            variant="ghost"
            color="gray"
            aria-label={collapsed ? `Expand ${label}` : `Collapse ${label}`}
            aria-expanded={!collapsed}
            onClick={onToggleCollapse}
          >
            {collapsed ? <ChevronRightIcon /> : <ChevronDownIcon />}
          </IconButton>
        </Flex>
      </Table.Cell>
    </Table.Row>
  )
}
