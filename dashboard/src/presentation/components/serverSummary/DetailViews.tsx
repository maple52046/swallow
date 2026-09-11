import { Card, Heading, Table } from '@chakra-ui/react'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import type { DetailSection, DetailTable } from '@/domain/server/types'

/** Provider-neutral OpenBMC/Ironic labelled section rendered without assuming field names. */
export function DetailSectionView({ section }: { section: DetailSection }) {
  if (!section.fields.length) return null
  return (
    <DescriptionList
      emptyText="No data"
      items={section.fields.map((field) => ({ label: field.label, value: field.value }))}
    />
  )
}

/** Provider-defined detail table shared by Network, Storage, PCI, and summary sections. */
export function DetailTableView({ table }: { table: DetailTable }) {
  if (!table.rows.length) {
    return (
      <EmptyState
        title={`No ${table.title.toLowerCase()}`}
        message={`The provisioner reported no ${table.title.toLowerCase()} rows.`}
      />
    )
  }
  return (
    <StickyTableFrame>
      <Table.Root size="sm" aria-label={table.title}>
        <Table.Header>
          <Table.Row>
            {table.columns.map((column) => (
              <Table.ColumnHeader key={column}>{column}</Table.ColumnHeader>
            ))}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {table.rows.map((row, rowIndex) => (
            <Table.Row key={rowIndex}>
              {row.map((cell, cellIndex) => (
                <Table.Cell key={cellIndex}>{cell || 'No data'}</Table.Cell>
              ))}
            </Table.Row>
          ))}
        </Table.Body>
      </Table.Root>
    </StickyTableFrame>
  )
}

/** Titled provider table card used in Cockpit-style summary sections. */
export function DetailTableCard({ table }: { table: DetailTable }) {
  return (
    <Card.Root>
      <Card.Body gap="4">
        <Heading size="sm">{table.title}</Heading>
        <DetailTableView table={table} />
      </Card.Body>
    </Card.Root>
  )
}

/** Standard partial-unavailable state for live provider detail. */
export function DetailUnavailable({ message }: { message: string }) {
  return <EmptyState title="Provisioner detail unavailable" message={message} />
}
