import { Table, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { AccessAuditEntry } from '@/domain/access/accounts'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { formatDateTime } from '@/shared/utils/time'
import { auditActionLabel } from './accountPresentation'

/** How many recent changes the section shows; the API keeps a year of them. */
const AUDIT_LIMIT = 50

/**
 * The most recent access changes on the Users page (access.md, admins only): who created, changed,
 * or deleted a User, who changed a Pool's members or sudo defaults, and who changed the access
 * settings. Entries never carry a password. `reloadKey` changes after each write on the page, so the
 * list shows the change just made.
 */
export function AccessAuditSection({ reloadKey }: { reloadKey: number }) {
  const { accounts } = useApp()
  const audit = useAsyncData(() => accounts.listAccessAudit(AUDIT_LIMIT), [accounts, reloadKey], { unavailableStatuses: [] })

  return (
    <SectionSurface
      title="Recent access changes"
      description={`The last ${AUDIT_LIMIT} changes to users, Pool members, and sudo rules, newest first.`}
      flush={audit.status === 'ready' && audit.data.length > 0}
    >
      {audit.status === 'loading' && <LoadingState rows={3} />}
      {(audit.status === 'error' || audit.status === 'unavailable') && (
        <ErrorState message={audit.message} requestId={audit.status === 'error' ? audit.requestId : undefined} onRetry={audit.reload} />
      )}
      {audit.status === 'ready' && (audit.data.length === 0
        ? <EmptyState title="No changes yet" message="Changes to users and Pool access appear here." />
        : <AuditList entries={audit.data} />)}
    </SectionSurface>
  )
}

/** The entries as a table on desktop and cards on mobile, built from the same values. */
function AuditList({ entries }: { entries: AccessAuditEntry[] }) {
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Recent access changes">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>When</Table.ColumnHeader>
                <Table.ColumnHeader>Who</Table.ColumnHeader>
                <Table.ColumnHeader>Change</Table.ColumnHeader>
                <Table.ColumnHeader>Target</Table.ColumnHeader>
                <Table.ColumnHeader>Detail</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {entries.map((entry) => (
                <Table.Row key={entry.id}>
                  <Table.Cell whiteSpace="nowrap">{formatDateTime(entry.at)}</Table.Cell>
                  <Table.Cell>{entry.actor}</Table.Cell>
                  <Table.Cell>{auditActionLabel(entry.action)}</Table.Cell>
                  <Table.Cell>{entry.target}</Table.Cell>
                  <Table.Cell><Text color="fg.muted" fontSize="sm">{entry.detail}</Text></Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label="Recent access changes">
          {entries.map((entry) => (
            <ResourceCard key={entry.id} title={auditActionLabel(entry.action)} description={formatDateTime(entry.at)}>
              <ResourceCardField label="Who">{entry.actor}</ResourceCardField>
              <ResourceCardField label="Target">{entry.target}</ResourceCardField>
              {entry.detail && <ResourceCardField label="Detail">{entry.detail}</ResourceCardField>}
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
