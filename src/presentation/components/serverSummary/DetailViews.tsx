import { Card, Flex, Heading, Table, Text } from '@radix-ui/themes'
import { EmptyState } from '@/presentation/components/EmptyState'
import type { DetailSection, DetailTable } from '@/domain/server/types'

/**
 * Shared renderers for the provider-neutral `provisioner-detail` shape (labelled sections
 * and generic tables). The Summary tab reuses `DetailTableCard` for its NUMA/Network
 * cards, and the Network/Storage/PCI tabs reuse it for the full tables — one renderer, so
 * every provider detail table looks the same.
 */

/** Renders one detail section (label/value pairs) as a two-column labelled grid. */
export function DetailSectionView({ section }: { section: DetailSection }) {
  if (section.fields.length === 0) return null
  return (
    <Flex direction="column" gap="2">
      <Heading as="h2" size="3">
        {section.title}
      </Heading>
      <Flex direction="column" gap="1">
        {section.fields.map((field) => (
          <Flex key={field.label} justify="between" gap="4">
            <Text size="2" color="gray">
              {field.label}
            </Text>
            <Text size="2" style={{ textAlign: 'right' }}>
              {field.value}
            </Text>
          </Flex>
        ))}
      </Flex>
    </Flex>
  )
}

/** Renders one detail table with its provider-defined columns. */
export function DetailTableView({ table }: { table: DetailTable }) {
  if (table.rows.length === 0) {
    return (
      <Text size="2" color="gray">
        No {table.title.toLowerCase()} reported.
      </Text>
    )
  }
  return (
    <Table.Root variant="surface">
      <Table.Header>
        <Table.Row>
          {table.columns.map((column) => (
            <Table.ColumnHeaderCell key={column}>{column}</Table.ColumnHeaderCell>
          ))}
        </Table.Row>
      </Table.Header>
      <Table.Body>
        {table.rows.map((row, rowIndex) => (
          <Table.Row key={rowIndex}>
            {row.map((cell, cellIndex) => (
              <Table.Cell key={cellIndex}>{cell}</Table.Cell>
            ))}
          </Table.Row>
        ))}
      </Table.Body>
    </Table.Root>
  )
}

/** A titled card wrapping one detail table, used by the Summary tab (NUMA, Network). */
export function DetailTableCard({ table }: { table: DetailTable }) {
  return (
    <Card>
      <Heading as="h2" size="3" mb="2">
        {table.title}
      </Heading>
      <DetailTableView table={table} />
    </Card>
  )
}

/** Standard "no live detail" surface for a tab when the provisioner could not be read. */
export function DetailUnavailable({ message }: { message: string }) {
  return (
    <EmptyState
      title="Provisioner detail unavailable"
      message={message}
    />
  )
}
