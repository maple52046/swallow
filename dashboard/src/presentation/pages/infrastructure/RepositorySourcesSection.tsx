import { Box, Table, Text } from '@chakra-ui/react'
import type { RepositorySource } from '@/domain/localRepository/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { declarersText, formatContentCount, wordListText } from './localRepositoryPresentation'
import { RepositorySyncStatus } from './RepositorySyncStatus'

/** A deb source's suites, components, and architectures on one line each; other formats have none. */
function SourceScope({ source }: { source: RepositorySource }) {
  if (source.suites.length === 0 && source.components.length === 0 && source.architectures.length === 0) return <>—</>
  return (
    <Box fontSize="sm">
      {source.suites.length > 0 && <div>Suites: {wordListText(source.suites)}</div>}
      {source.components.length > 0 && <div>Components: {wordListText(source.components)}</div>}
      {source.architectures.length > 0 && <Text color="fg.muted">Architectures: {wordListText(source.architectures)}</Text>}
    </Box>
  )
}

/** When the source last synced successfully, relative with the exact time as its title; "Never" before one. */
function LastSynced({ at }: { at: string | null }) {
  if (!at) return <>Never</>
  return <time dateTime={at} title={formatDateTime(at)}>{formatRelative(at)}</time>
}

/**
 * The effective Repository Sources of a Site's Local Repository (observed state): every source a
 * module declares, the user's sources, and the pull-through caches swallow derives, in the API's
 * order (by name). Each row shows who declared it — a module by its id, the user's spec as "User" —
 * and the worker's last observation: the state with the provider's message as text, the last
 * successful sync, and how many items it serves (`—` when unknown). Read-only: the user's own
 * sources are edited through the User sources section, and a module's change with the module.
 * Desktop rows and mobile cards show the same facts.
 */
export function RepositorySourcesSection({ sources }: { sources: readonly RepositorySource[] }) {
  return (
    <SectionSurface
      title="Repository sources"
      description="The upstreams the repository mirrors or caches, as the worker last observed them."
      className="sw-local-repository-section"
      flush
    >
      {sources.length === 0 ? (
        <div className="sw-section-empty">
          <strong>No sources</strong>
          <span>No module declares a source and you have added none.</span>
        </div>
      ) : (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Repository sources">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Format</Table.ColumnHeader>
                    <Table.ColumnHeader>Suites and components</Table.ColumnHeader>
                    <Table.ColumnHeader>Declared by</Table.ColumnHeader>
                    <Table.ColumnHeader>State</Table.ColumnHeader>
                    <Table.ColumnHeader>Last synced</Table.ColumnHeader>
                    <Table.ColumnHeader textAlign="end">Items</Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {sources.map((source) => (
                    <Table.Row key={source.name} id={`repository-source-${source.name}`}>
                      <Table.Cell maxW="22rem">
                        <strong>{source.name}</strong>
                        <Text as="small" display="block" color="fg.muted" className="sw-mono sw-local-repository__wrap">
                          {source.url}
                        </Text>
                      </Table.Cell>
                      <Table.Cell className="sw-mono">{source.format}</Table.Cell>
                      <Table.Cell><SourceScope source={source} /></Table.Cell>
                      <Table.Cell>{declarersText(source.declaredBy)}</Table.Cell>
                      <Table.Cell maxW="20rem"><RepositorySyncStatus state={source.state} message={source.message} /></Table.Cell>
                      <Table.Cell><LastSynced at={source.lastSyncedAt} /></Table.Cell>
                      <Table.Cell textAlign="end">{formatContentCount(source.contentCount)}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Repository sources">
              {sources.map((source) => (
                <ResourceCard
                  key={source.name}
                  title={source.name}
                  description={<span className="sw-mono sw-local-repository__wrap">{source.url}</span>}
                  status={<RepositorySyncStatus state={source.state} message={null} />}
                >
                  <ResourceCardField label="Format">{source.format}</ResourceCardField>
                  <ResourceCardField label="Declared by">{declarersText(source.declaredBy)}</ResourceCardField>
                  <ResourceCardField label="Last synced"><LastSynced at={source.lastSyncedAt} /></ResourceCardField>
                  <ResourceCardField label="Items">{formatContentCount(source.contentCount)}</ResourceCardField>
                  {(source.suites.length > 0 || source.components.length > 0) && (
                    <ResourceCardField label="Suites and components"><SourceScope source={source} /></ResourceCardField>
                  )}
                  {source.message && (
                    <ResourceCardField label={source.state === 'failed' ? 'Failure' : 'Message'}>
                      <Text color={source.state === 'failed' ? 'fg.error' : 'fg.muted'}>{source.message}</Text>
                    </ResourceCardField>
                  )}
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}
    </SectionSurface>
  )
}
