import type { ReactNode } from 'react'
import { Box, HStack, Table, Text } from '@chakra-ui/react'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'

/** One data column shared by the desktop table and the mobile card. */
export interface ResponsiveResourceColumn<T> {
  header: string
  cell: (item: T) => ReactNode
}

interface ResponsiveResourceListProps<T> {
  /** Accessible name of the table and the mobile list, e.g. "Docker images". */
  label: string
  items: readonly T[]
  rowKey: (item: T) => string
  /** Header of the identity column (the first column on desktop, the card title on mobile). */
  identityHeader: string
  title: (item: T) => ReactNode
  /** Secondary identity text, such as a short id. */
  subtitle?: (item: T) => ReactNode
  /** Rendered in the card header on mobile and as its own column on desktop when given. */
  status?: { header: string; cell: (item: T) => ReactNode }
  columns: readonly ResponsiveResourceColumn<T>[]
  actions: (item: T) => ReactNode
}

/**
 * Column-driven resource list: a caller describes its columns once and gets a dense desktop table
 * and equivalent mobile cards through `ResponsiveDataView`.
 *
 * Used by the Docker Host Explorer sections (images, containers, volumes, networks) and the Registry
 * credentials section, so those lists cannot drift apart and narrow viewports keep every fact and
 * action. The identity column (title plus optional subtitle) comes first on desktop and is the card
 * title on mobile; `status` gets its own column on desktop and sits in the card header on mobile.
 * Actions are always visible (never hover-only). Items render in the order given.
 */
export function ResponsiveResourceList<T>({ label, items, rowKey, identityHeader, title, subtitle, status, columns, actions }: ResponsiveResourceListProps<T>) {
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label={label}>
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>{identityHeader}</Table.ColumnHeader>
                {status && <Table.ColumnHeader>{status.header}</Table.ColumnHeader>}
                {columns.map((column) => (
                  <Table.ColumnHeader key={column.header}>{column.header}</Table.ColumnHeader>
                ))}
                <Table.ColumnHeader>Actions</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {items.map((item) => (
                <Table.Row key={rowKey(item)}>
                  <Table.Cell>
                    <Box minW="0">
                      <Box fontWeight="semibold">{title(item)}</Box>
                      {subtitle && (
                        <Text color="fg.muted" fontSize="xs" className="mono">
                          {subtitle(item)}
                        </Text>
                      )}
                    </Box>
                  </Table.Cell>
                  {status && <Table.Cell>{status.cell(item)}</Table.Cell>}
                  {columns.map((column) => (
                    <Table.Cell key={column.header}>{column.cell(item)}</Table.Cell>
                  ))}
                  <Table.Cell>
                    <HStack gap="2" wrap="wrap">
                      {actions(item)}
                    </HStack>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <Box as="ul" listStyleType="none" display="grid" gap="3" aria-label={label}>
          {items.map((item) => (
            <Box as="li" key={rowKey(item)}>
              <ResourceCard
                title={title(item)}
                description={subtitle?.(item)}
                status={status?.cell(item)}
                actions={actions(item)}
              >
                {columns.map((column) => (
                  <ResourceCardField key={column.header} label={column.header}>
                    {column.cell(item)}
                  </ResourceCardField>
                ))}
              </ResourceCard>
            </Box>
          ))}
        </Box>
      }
    />
  )
}
