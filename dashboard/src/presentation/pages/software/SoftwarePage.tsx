import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Box, Button, Heading, HStack, Stack, Text } from '@chakra-ui/react'
import { ArrowRight } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { loadSoftwareFootprint, type SoftwareFootprintWorkingSet } from '@/application/usecases/software/loadSoftwareWorkspace'
import { useApp } from '@/di/AppProvider'
import type { SoftwareCatalogEntry } from '@/domain/software/types'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import {
  groupSoftwareCatalog,
  isSoftwareAssignmentChanging,
  softwareCatalogPresentation,
  softwareFootprints,
} from './softwareCatalogPresentation'
import './software.css'

type CatalogState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; catalog: SoftwareCatalogEntry[] }

type FootprintState =
  | { status: 'loading' }
  | { status: 'error'; message: string; data?: SoftwareFootprintWorkingSet }
  | { status: 'ready'; data: SoftwareFootprintWorkingSet }

const REFRESH_INTERVAL_MS = 5_000

/**
 * Managed Software catalog route.
 *
 * Software identity remains available when Site-scoped deployment context cannot be read: the
 * catalog and footprint have separate state boundaries. Cards are native links so the entire
 * discovery surface is keyboard-accessible without turning nested actions into invalid markup.
 */
export function SoftwarePage() {
  const { software, servers } = useApp()
  const { siteId, scopedHref } = useSiteScope()
  const [catalogState, setCatalogState] = useState<CatalogState>({ status: 'loading' })
  const [footprintState, setFootprintState] = useState<FootprintState>({ status: 'loading' })

  const loadCatalog = useCallback(async () => {
    setCatalogState({ status: 'loading' })
    try {
      setCatalogState({ status: 'ready', catalog: await software.listCatalog() })
    } catch (caught) {
      setCatalogState({
        status: 'error',
        message: caught instanceof Error ? caught.message : 'The software catalog could not be loaded.',
      })
    }
  }, [software])

  useEffect(() => {
    let cancelled = false
    void software.listCatalog().then(
      (catalog) => {
        if (!cancelled) setCatalogState({ status: 'ready', catalog })
      },
      (caught: unknown) => {
        if (!cancelled) {
          setCatalogState({
            status: 'error',
            message: caught instanceof Error ? caught.message : 'The software catalog could not be loaded.',
          })
        }
      },
    )
    return () => {
      cancelled = true
    }
  }, [software])

  const refreshFootprint = useCallback(async (background = false) => {
    if (!background) setFootprintState({ status: 'loading' })
    try {
      const data = await loadSoftwareFootprint(software, servers, siteId)
      setFootprintState({ status: 'ready', data })
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : 'Deployment data could not be loaded.'
      setFootprintState((current) => ({
        status: 'error',
        message,
        data: current.status === 'ready' || current.status === 'error' ? current.data : undefined,
      }))
    }
  }, [software, servers, siteId])

  useEffect(() => {
    let cancelled = false
    void Promise.resolve().then(() => {
      if (!cancelled) setFootprintState({ status: 'loading' })
    })
    void loadSoftwareFootprint(software, servers, siteId).then(
      (data) => {
        if (!cancelled) setFootprintState({ status: 'ready', data })
      },
      (caught: unknown) => {
        if (!cancelled) {
          setFootprintState({
            status: 'error',
            message: caught instanceof Error ? caught.message : 'Deployment data could not be loaded.',
          })
        }
      },
    )
    return () => {
      cancelled = true
    }
  }, [software, servers, siteId])

  const footprintData = footprintState.status === 'ready' || footprintState.status === 'error'
    ? footprintState.data
    : undefined
  const scopedServerIds = useMemo(
    () => new Set(footprintData?.servers.map((server) => server.id) ?? []),
    [footprintData],
  )
  const hasChanging = footprintData?.assignments.some((assignment) =>
    scopedServerIds.has(assignment.serverId) && isSoftwareAssignmentChanging(assignment)
  ) ?? false

  useEffect(() => {
    if (!hasChanging) return
    const timer = window.setTimeout(() => void refreshFootprint(true), REFRESH_INTERVAL_MS)
    return () => window.clearTimeout(timer)
  }, [hasChanging, refreshFootprint, footprintState])

  const groups = useMemo(
    () => catalogState.status === 'ready' ? groupSoftwareCatalog(catalogState.catalog) : [],
    [catalogState],
  )
  const footprints = useMemo(
    () => catalogState.status === 'ready' && footprintData
      ? softwareFootprints(catalogState.catalog, footprintData.assignments, footprintData.servers)
      : new Map(),
    [catalogState, footprintData],
  )

  return (
    <div className="operator-page sw-software-catalog-page">
      <PageHeader
        title="Software"
        subtitle="Discover and manage host-level software that Swallow can install on deployed Servers."
      />

      {footprintState.status === 'error' && (
        <Alert status="warning" title="Deployment context is temporarily unavailable">
          <HStack justify="space-between" gap="3" align="flex-start" wrap="wrap">
            <Text>{footprintState.message} The installable software catalog is still available.</Text>
            <Button size="xs" variant="outline" onClick={() => void refreshFootprint(false)}>Retry</Button>
          </HStack>
        </Alert>
      )}

      {catalogState.status === 'loading' && <LoadingState rows={3} />}
      {catalogState.status === 'error' && <ErrorState message={catalogState.message} onRetry={() => void loadCatalog()} />}
      {catalogState.status === 'ready' && catalogState.catalog.length === 0 && (
        <ErrorState message="The active installation does not publish any Managed Software." onRetry={() => void loadCatalog()} />
      )}
      {catalogState.status === 'ready' && catalogState.catalog.length > 0 && (
        <Stack gap="10">
          {groups.map((group) => (
            <Box as="section" key={group.key} aria-labelledby={`software-category-${group.key}`}>
              <Box className="sw-software-category-heading">
                <Heading as="h2" id={`software-category-${group.key}`} size="lg">{group.label}</Heading>
                <Text color="fg.muted">{group.description}</Text>
              </Box>
              <div className="sw-software-catalog-grid">
                {group.entries.map((entry) => {
                  const presentation = softwareCatalogPresentation(entry.kind)
                  const footprint = footprints.get(entry.kind)
                  const Icon = presentation.icon
                  return (
                    <RouterLink
                      key={entry.kind}
                      to={scopedHref(`/software/${entry.kind}`)}
                      className="sw-software-catalog-card"
                      aria-label={`Open ${entry.label}`}
                    >
                      <div className="sw-software-catalog-card__topline">
                        <span className="sw-software-catalog-card__icon" aria-hidden><Icon size={25} /></span>
                        <Badge variant="subtle" colorPalette="gray">{group.label}</Badge>
                      </div>
                      <div>
                        <Heading as="h3" size="lg">{entry.label}</Heading>
                        <Text color="fg.muted" mt="2">{presentation.description}</Text>
                      </div>
                      <HStack gap="2" wrap="wrap" aria-label={`${entry.label} capabilities`}>
                        {presentation.capabilities.slice(0, 2).map((capability) => (
                          <Badge key={capability} variant="outline" colorPalette="gray">{capability}</Badge>
                        ))}
                      </HStack>
                      <div className="sw-software-catalog-card__footer">
                        <div>
                          {footprint ? (
                            <>
                              <Text fontWeight="semibold">Installed on {footprint.installed} {footprint.installed === 1 ? 'Server' : 'Servers'}</Text>
                              {(footprint.changing > 0 || footprint.failed > 0) && (
                                <Text color={footprint.failed > 0 ? 'red.fg' : 'fg.muted'} fontSize="sm">
                                  {[
                                    footprint.changing > 0 ? `${footprint.changing} changing` : '',
                                    footprint.failed > 0 ? `${footprint.failed} failed` : '',
                                  ].filter(Boolean).join(' · ')}
                                </Text>
                              )}
                            </>
                          ) : (
                            <Text color="fg.muted">Deployment count unavailable</Text>
                          )}
                        </div>
                        <ArrowRight size={18} aria-hidden />
                      </div>
                    </RouterLink>
                  )
                })}
              </div>
            </Box>
          ))}
        </Stack>
      )}
    </div>
  )
}
