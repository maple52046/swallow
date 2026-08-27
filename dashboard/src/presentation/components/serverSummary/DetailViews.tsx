import { Card, CardBody, CardTitle, DescriptionList, DescriptionListDescription, DescriptionListGroup, DescriptionListTerm } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import type { DetailSection, DetailTable } from '@/domain/server/types'

/** Provider-neutral OpenBMC/Ironic labelled section rendered without assuming field names. */
export function DetailSectionView({ section }: { section: DetailSection }) {
  if (!section.fields.length) return null
  return <DescriptionList isHorizontal isCompact>{section.fields.map((field) => <DescriptionListGroup key={field.label}><DescriptionListTerm>{field.label}</DescriptionListTerm><DescriptionListDescription>{field.value || 'No data'}</DescriptionListDescription></DescriptionListGroup>)}</DescriptionList>
}

/** Provider-defined detail table shared by Network, Storage, PCI, and summary sections. */
export function DetailTableView({ table }: { table: DetailTable }) {
  if (!table.rows.length) return <EmptyState title={`No ${table.title.toLowerCase()}`} message={`The provisioner reported no ${table.title.toLowerCase()} rows.`} />
  return <StickyTableFrame><Table aria-label={table.title} variant="compact"><Thead><Tr>{table.columns.map((column) => <Th key={column}>{column}</Th>)}</Tr></Thead><Tbody>{table.rows.map((row, rowIndex) => <Tr key={rowIndex}>{row.map((cell, cellIndex) => <Td key={cellIndex} dataLabel={table.columns[cellIndex]}>{cell || 'No data'}</Td>)}</Tr>)}</Tbody></Table></StickyTableFrame>
}

/** Titled provider table card used in Cockpit-style summary sections. */
export function DetailTableCard({ table }: { table: DetailTable }) {
  return <Card><CardTitle>{table.title}</CardTitle><CardBody><DetailTableView table={table} /></CardBody></Card>
}

/** Standard partial-unavailable state for live provider detail. */
export function DetailUnavailable({ message }: { message: string }) {
  return <EmptyState title="Provisioner detail unavailable" message={message} />
}
