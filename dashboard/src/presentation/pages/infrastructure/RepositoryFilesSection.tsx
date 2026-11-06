import { HStack, Table, Text } from '@chakra-ui/react'
import type { LocalRepositoryFile } from '@/domain/localRepository/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { CopyButton } from '@/presentation/components/CopyButton'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { declarersText, formatReportedSize, shortDigest } from './localRepositoryPresentation'
import { RepositorySyncStatus } from './RepositorySyncStatus'

/** A file's declared sha256, shortened for the table, with the full value one click away. */
function Digest({ file }: { file: LocalRepositoryFile }) {
  return (
    <HStack gap="1">
      <span className="sw-mono" title={file.sha256}>{shortDigest(file.sha256)}</span>
      <CopyButton value={file.sha256} label={`Copy sha256 of ${file.path}`} />
    </HStack>
  )
}

/**
 * The files the modules declare in a Site's Local Repository, ordered by path: each is downloaded
 * once from its URL and checked against its declared sha256, so the table shows the digest
 * (shortened, with a copy action for the full value), who declared it, the worker's observed state
 * with the provider's message as text (a checksum mismatch reads there), and the size once fetched
 * (`—` before). Read-only: files change with the module that declares them.
 */
export function RepositoryFilesSection({ files }: { files: readonly LocalRepositoryFile[] }) {
  return (
    <SectionSurface
      title="Files"
      description="Single files the modules declare, each verified against its declared sha256."
      className="sw-local-repository-section"
      flush
    >
      {files.length === 0 ? (
        <div className="sw-section-empty">
          <strong>No files</strong>
          <span>No module of this build declares a file.</span>
        </div>
      ) : (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Declared files">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Path</Table.ColumnHeader>
                    <Table.ColumnHeader>sha256</Table.ColumnHeader>
                    <Table.ColumnHeader>Declared by</Table.ColumnHeader>
                    <Table.ColumnHeader>State</Table.ColumnHeader>
                    <Table.ColumnHeader textAlign="end">Size</Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {files.map((file) => (
                    <Table.Row key={file.path}>
                      <Table.Cell maxW="26rem">
                        <Text className="sw-mono sw-local-repository__wrap">{file.path}</Text>
                        <Text as="small" display="block" color="fg.muted" className="sw-mono sw-local-repository__wrap">
                          {file.url}
                        </Text>
                      </Table.Cell>
                      <Table.Cell><Digest file={file} /></Table.Cell>
                      <Table.Cell>{declarersText(file.declaredBy)}</Table.Cell>
                      <Table.Cell maxW="20rem"><RepositorySyncStatus state={file.state} message={file.message} /></Table.Cell>
                      <Table.Cell textAlign="end">{formatReportedSize(file.sizeBytes)}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Declared files">
              {files.map((file) => (
                <ResourceCard
                  key={file.path}
                  title={<span className="sw-mono sw-local-repository__wrap">{file.path}</span>}
                  description={<span className="sw-mono sw-local-repository__wrap">{file.url}</span>}
                  status={<RepositorySyncStatus state={file.state} message={null} />}
                >
                  <ResourceCardField label="sha256"><Digest file={file} /></ResourceCardField>
                  <ResourceCardField label="Size">{formatReportedSize(file.sizeBytes)}</ResourceCardField>
                  <ResourceCardField label="Declared by">{declarersText(file.declaredBy)}</ResourceCardField>
                  {file.message && (
                    <ResourceCardField label={file.state === 'failed' ? 'Failure' : 'Message'}>
                      <Text color={file.state === 'failed' ? 'fg.error' : 'fg.muted'}>{file.message}</Text>
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
