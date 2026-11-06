import { Table, Text } from '@chakra-ui/react'
import type { LocalRepositoryImage } from '@/domain/localRepository/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { declarersText, formatReportedSize } from './localRepositoryPresentation'
import { RepositorySyncStatus } from './RepositorySyncStatus'

/**
 * The container images the modules declare in a Site's Local Repository, ordered by reference, as
 * the worker last observed them. Each is fetched in advance through its registry's pull-through
 * source and counts toward the repository's readiness, so the table shows the observed state with
 * the provider's message as text (`Pending` while its source is not ready, `Failed` with the reason
 * when a fetch failed), the total size once every layer is stored (`—` before), and who declared it.
 * The states and sizes are the API's; the dashboard derives nothing. Read-only: images change with
 * the module that declares them. Desktop rows and mobile cards show the same facts.
 */
export function RepositoryImagesSection({ images }: { images: readonly LocalRepositoryImage[] }) {
  return (
    <SectionSurface
      title="Declared images"
      description="Fetched in advance through their registry's pull-through source; ready once every layer is stored."
      className="sw-local-repository-section"
      flush
    >
      {images.length === 0 ? (
        <div className="sw-section-empty">
          <strong>No images</strong>
          <span>No module of this build declares a container image.</span>
        </div>
      ) : (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Declared images">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Image</Table.ColumnHeader>
                    <Table.ColumnHeader>Declared by</Table.ColumnHeader>
                    <Table.ColumnHeader>State</Table.ColumnHeader>
                    <Table.ColumnHeader textAlign="end">Size</Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {images.map((image) => (
                    <Table.Row key={image.reference}>
                      <Table.Cell maxW="30rem">
                        <Text className="sw-mono sw-local-repository__wrap">{image.reference}</Text>
                      </Table.Cell>
                      <Table.Cell>{declarersText(image.declaredBy)}</Table.Cell>
                      <Table.Cell maxW="24rem"><RepositorySyncStatus state={image.state} message={image.message} /></Table.Cell>
                      <Table.Cell textAlign="end">{formatReportedSize(image.sizeBytes)}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Declared images">
              {images.map((image) => (
                <ResourceCard
                  key={image.reference}
                  title={<span className="sw-mono sw-local-repository__wrap">{image.reference}</span>}
                  status={<RepositorySyncStatus state={image.state} message={null} />}
                >
                  <ResourceCardField label="Size">{formatReportedSize(image.sizeBytes)}</ResourceCardField>
                  <ResourceCardField label="Declared by">{declarersText(image.declaredBy)}</ResourceCardField>
                  {image.message && (
                    <ResourceCardField label={image.state === 'failed' ? 'Failure' : 'Message'}>
                      <Text color={image.state === 'failed' ? 'fg.error' : 'fg.muted'}>{image.message}</Text>
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
