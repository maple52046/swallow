import { useCallback, useEffect, useMemo } from 'react'
import { Box, Table, Text } from '@chakra-ui/react'
import { useSearchParams } from 'react-router-dom'
import {
  BROWSABLE_FORMATS,
  type LocalRepositoryContentItem,
  type LocalRepositoryFormat,
  type RepositorySource,
} from '@/domain/localRepository/types'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { Pagination } from '@/presentation/components/Pagination'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { DebouncedSearchInput } from '@/presentation/components/ui/debounced-search-input'
import { Select } from '@/presentation/components/ui/select'
import { FORMAT_LABELS, FORMAT_NOUNS, formatReportedSize } from './localRepositoryPresentation'
import { useLocalRepositoryContent } from './useLocalRepositoryContent'

/** Items per content page; within the contract's page-size range. */
const PAGE_SIZE = 25

/**
 * The URL parameters the browser owns, so a view of the content can be shared and survives a
 * reload: `format` (default `deb`), `source`, `q` (the name search), and `page` (1-based). `site`
 * belongs to the global Site scope.
 */
const PARAM = { format: 'format', source: 'source', search: 'q', page: 'page' } as const

/** The optional fields of a content item, each a column when an item of the page has it. */
type OptionalColumn = 'version' | 'architecture' | 'source' | 'path' | 'sizeBytes'

const OPTIONAL_COLUMNS: ReadonlyArray<{ field: OptionalColumn; header: string; render: (item: LocalRepositoryContentItem) => string; mono?: boolean }> = [
  { field: 'version', header: 'Version', render: (item) => item.version ?? '—', mono: true },
  { field: 'architecture', header: 'Architecture', render: (item) => item.architecture ?? '—' },
  { field: 'source', header: 'Source', render: (item) => item.source ?? '—' },
  { field: 'path', header: 'Path', render: (item) => item.path ?? '—', mono: true },
  { field: 'sizeBytes', header: 'Size', render: (item) => formatReportedSize(item.sizeBytes) },
]

function isBrowsableFormat(value: string | null): value is LocalRepositoryFormat {
  return value !== null && (BROWSABLE_FORMATS as readonly string[]).includes(value)
}

/**
 * Items of one page are told apart by every field they carry; the provider does not send an id.
 * Two items with all fields equal would be the same artifact, so the key stays unique in practice.
 */
function itemKey(item: LocalRepositoryContentItem): string {
  return [item.name, item.version, item.architecture, item.source, item.path].join('|')
}

interface LocalRepositoryContentBrowserProps {
  siteId: string
  /** The effective Repository Sources, whose names of the chosen format feed the source filter. */
  sources: readonly RepositorySource[]
}

/**
 * The content browser of the Local repository page: one page at a time of what the provider
 * serves, read live through the API, by format, optionally narrowed to one source and a name
 * search.
 *
 * Paging is server-side with the shared Pagination, and the URL owns the format, source, search,
 * and page like the Server list owns its filters: a changed format clears the source and page, a
 * changed source or search the page, and values that no longer apply (an unknown format, a source
 * the repository does not have for the format, a page past the end) are canonicalized away by
 * replacing the history entry. Columns are the fields present on the current page, because the
 * contract leaves which formats carry which fields to the provider. Loading, a provider failure
 * (with its message and request ID, and a retry), and an empty result use the shared states; the
 * result count is announced as a polite status.
 */
export function LocalRepositoryContentBrowser({ siteId, sources }: LocalRepositoryContentBrowserProps) {
  const [params, setParams] = useSearchParams()
  const paramsKey = params.toString()
  const rawFormat = params.get(PARAM.format)
  const format: LocalRepositoryFormat = isBrowsableFormat(rawFormat) ? rawFormat : 'deb'
  const source = params.get(PARAM.source) ?? ''
  const search = params.get(PARAM.search) ?? ''
  const rawPage = params.get(PARAM.page)
  const page = Math.max(1, Math.floor(Number(rawPage)) || 1)

  const formatSources = useMemo(() => sources.filter((item) => item.format === format), [format, sources])
  const sourceKnown = source === '' || formatSources.some((item) => item.name === source)

  const updateParams = useCallback((update: (next: URLSearchParams) => void, replace = false) => {
    const next = new URLSearchParams(paramsKey)
    update(next)
    setParams(next, { replace })
  }, [paramsKey, setParams])

  // Canonicalize values that cannot apply, replacing the history entry so Back skips them.
  useEffect(() => {
    const formatInvalid = rawFormat !== null && !isBrowsableFormat(rawFormat)
    const sourceInvalid = !sourceKnown
    const pageNonCanonical = rawPage !== null && (page === 1 || String(page) !== rawPage)
    if (!formatInvalid && !sourceInvalid && !pageNonCanonical) return
    updateParams((next) => {
      if (formatInvalid) next.delete(PARAM.format)
      if (sourceInvalid) next.delete(PARAM.source)
      if (page > 1 && !formatInvalid && !sourceInvalid) next.set(PARAM.page, String(page))
      else next.delete(PARAM.page)
    }, true)
  }, [page, rawFormat, rawPage, sourceKnown, updateParams])

  const query = useMemo(() => ({
    format,
    source: sourceKnown && source ? source : undefined,
    search: search || undefined,
    page,
    pageSize: PAGE_SIZE,
  }), [format, page, search, source, sourceKnown])
  const { state, reload } = useLocalRepositoryContent(siteId, query)

  // A page past the end (the content shrank, or a stale link) moves to the last page there is.
  useEffect(() => {
    if (state.status !== 'ready' || state.page.items.length > 0 || page === 1) return
    const lastPage = Math.max(1, Math.ceil(state.page.total / PAGE_SIZE))
    if (page <= lastPage) return
    updateParams((next) => {
      if (lastPage > 1) next.set(PARAM.page, String(lastPage))
      else next.delete(PARAM.page)
    }, true)
  }, [page, state, updateParams])

  const changeFormat = (value: string) => {
    if (!isBrowsableFormat(value) || value === format) return
    updateParams((next) => {
      next.set(PARAM.format, value)
      next.delete(PARAM.source)
      next.delete(PARAM.page)
    })
  }
  const changeSource = (value: string) => {
    updateParams((next) => {
      if (value) next.set(PARAM.source, value)
      else next.delete(PARAM.source)
      next.delete(PARAM.page)
    })
  }
  const commitSearch = useCallback((value: string) => {
    updateParams((next) => {
      if (value) next.set(PARAM.search, value)
      else next.delete(PARAM.search)
      next.delete(PARAM.page)
    }, true)
  }, [updateParams])
  const changePage = (nextPage: number) => {
    updateParams((next) => {
      if (nextPage > 1) next.set(PARAM.page, String(nextPage))
      else next.delete(PARAM.page)
    })
  }
  const clearFilters = () => {
    updateParams((next) => {
      next.delete(PARAM.source)
      next.delete(PARAM.search)
      next.delete(PARAM.page)
    })
  }

  const filtered = Boolean(query.source || query.search)
  const items = state.status === 'ready' ? state.page.items : []
  const columns = OPTIONAL_COLUMNS.filter((column) => items.some((item) => item[column.field] !== null))
  const total = state.status === 'ready' ? state.page.total : 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const rangeStart = items.length > 0 ? (page - 1) * PAGE_SIZE + 1 : 0
  const rangeEnd = items.length > 0 ? rangeStart + items.length - 1 : 0
  const formatLabel = FORMAT_LABELS[format]
  const formatNoun = FORMAT_NOUNS[format]
  const summary = state.status === 'ready'
    ? total === 0 ? `No ${formatNoun}` : `Showing ${rangeStart}–${rangeEnd} of ${total} ${formatNoun}`
    : state.status === 'error' ? 'Content unavailable' : 'Loading content'

  return (
    <SectionSurface
      title="Content"
      description="What the repository serves now, read live from it. Choose a format, then narrow it to one source or a name."
      className="sw-local-repository-content"
      flush
    >
      <div className="sw-local-repository-content__toolbar">
        <Select
          aria-label="Content format"
          value={format}
          size="sm"
          width="auto"
          onChange={changeFormat}
          options={BROWSABLE_FORMATS.map((value) => ({ value, label: FORMAT_LABELS[value] }))}
        />
        <Select
          aria-label="Source"
          value={sourceKnown ? source : ''}
          size="sm"
          width="auto"
          disabled={formatSources.length === 0}
          onChange={changeSource}
          options={[
            { value: '', label: formatSources.length === 0 ? 'No sources of this format' : 'All sources' },
            ...formatSources.map((item) => ({ value: item.name, label: item.name })),
          ]}
        />
        <Box flex="1" minW="12rem">
          <DebouncedSearchInput
            value={search}
            onCommit={commitSearch}
            placeholder="Search by name"
            aria-label="Search content by name"
            maxW="100%"
          />
        </Box>
        <Text className="sw-local-repository-content__summary" color="fg.muted" fontSize="sm" role="status" aria-live="polite" aria-atomic="true">
          {summary}
        </Text>
      </div>

      {state.status === 'loading' && <div className="sw-local-repository-content__state"><LoadingState rows={6} /></div>}
      {state.status === 'error' && (
        <div className="sw-local-repository-content__state">
          <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />
        </div>
      )}
      {state.status === 'ready' && items.length === 0 && (
        <div className="sw-local-repository-content__state">
          <EmptyState
            title={filtered ? 'Nothing matches' : `No ${formatNoun} yet`}
            message={
              filtered
                ? 'No item of this format matches the source and search. Clear them to see everything.'
                : 'The repository serves none of this format yet. Content appears as its sources sync.'
            }
            action={filtered ? { label: 'Clear source and search', onClick: clearFilters } : undefined}
          />
        </div>
      )}
      {state.status === 'ready' && items.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label={`${formatLabel} in the Local Repository`}>
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    {columns.map((column) => (
                      <Table.ColumnHeader key={column.field}>{column.header}</Table.ColumnHeader>
                    ))}
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {items.map((item) => (
                    <Table.Row key={itemKey(item)}>
                      <Table.Cell fontWeight="semibold" className="sw-local-repository__wrap">{item.name}</Table.Cell>
                      {columns.map((column) => (
                        <Table.Cell key={column.field} className={column.mono ? 'sw-mono sw-local-repository__wrap' : 'sw-local-repository__wrap'}>
                          {column.render(item)}
                        </Table.Cell>
                      ))}
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label={`${formatLabel} in the Local Repository`}>
              {items.map((item) => (
                <ResourceCard key={itemKey(item)} title={<span className="sw-local-repository__wrap">{item.name}</span>}>
                  {columns.map((column) => (
                    <ResourceCardField key={column.field} label={column.header}>
                      <span className={column.mono ? 'sw-mono' : undefined}>{column.render(item)}</span>
                    </ResourceCardField>
                  ))}
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}
      {state.status === 'ready' && totalPages > 1 && (
        <div className="sw-local-repository-content__pagination">
          <Pagination total={totalPages} value={page} onChange={changePage} subject="content" />
        </div>
      )}
    </SectionSurface>
  )
}
