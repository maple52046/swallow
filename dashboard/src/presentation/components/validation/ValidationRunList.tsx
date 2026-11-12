import { Button, Link as ChakraLink, Stack, Table, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import type { ValidationRunSummary } from '@/domain/validation/types'
import { StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { formatDateTime } from '@/shared/utils/time'
import { countsLabel, runDuration, runOutcome, runTriggerLabel } from './validationPresentation'

interface ValidationRunListProps {
  runs: readonly ValidationRunSummary[]
  label: string
  hideTarget?: boolean
  /** Shows Site identity in all-Sites inventories. */
  showSite?: boolean
}

/**
 * Responsive Validation Run inventory shared by the assurance list and resource detail pages.
 * Every navigation affordance is a real link; failed and error outcomes receive review wording
 * without merging their distinct domain verdicts.
 */
export function ValidationRunList({ runs, label, hideTarget = false, showSite = false }: ValidationRunListProps) {
  const { scopedHref, sites } = useSiteScope()
  const serverIds = runs.flatMap((run) => (run.target.platformId ? [] : run.target.serverIds))
  const serverNames = useServerNames(serverIds)
  const platformNames = usePlatformNamesById(runs.flatMap((run) => (run.target.platformId ? [run.target.platformId] : [])))
  const siteName = new Map(sites.map((site) => [site.id, site.name]))
  const targetLabel = (run: ValidationRunSummary) => {
    if (run.target.platformId) return `Platform ${platformNames.get(run.target.platformId) ?? run.target.platformId}`
    return run.target.serverIds.map((id) => serverNames.get(id) ?? id).join(', ')
  }
  const href = (run: ValidationRunSummary) => scopedHref(
    `/validation/runs/${encodeURIComponent(run.id)}`,
    run.siteId,
  )
  const identity = (run: ValidationRunSummary) => (
    <Stack gap="0.5">
      <ChakraLink asChild fontWeight="semibold" color="brand.fg">
        <RouterLink to={href(run)}>{run.suite.name}</RouterLink>
      </ChakraLink>
      <Text className="sw-mono" fontSize="xs" color="fg.muted">{run.id}</Text>
      <Text fontSize="xs" color="fg.muted">
        {run.suite.source === 'user' ? `Script revision ${run.suite.revision ?? '—'}` : run.suite.version ? `Module ${run.suite.version}` : 'Module suite'}
      </Text>
    </Stack>
  )
  const outcome = (run: ValidationRunSummary) => {
    const current = runOutcome(run)
    return <StatusBadge status={current.status} label={current.label} />
  }
  const action = (run: ValidationRunSummary) => {
    const review = run.verdict === 'failed' || run.verdict === 'error'
    return (
      <Button asChild size="sm" variant={review ? 'solid' : 'outline'} colorPalette={review ? 'brand' : undefined} minW="7rem">
        <RouterLink to={href(run)}>{review ? 'Review run' : 'View run'}</RouterLink>
      </Button>
    )
  }
  const target = (run: ValidationRunSummary) => (
    <Stack gap="0.5">
      <Text>{targetLabel(run)}</Text>
      {showSite && <Text fontSize="xs" color="fg.muted">Site: {siteName.get(run.siteId) ?? run.siteId}</Text>}
    </Stack>
  )

  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label={label}>
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Validation</Table.ColumnHeader>
                {!hideTarget && <Table.ColumnHeader>Target</Table.ColumnHeader>}
                <Table.ColumnHeader>Outcome</Table.ColumnHeader>
                <Table.ColumnHeader>Findings</Table.ColumnHeader>
                <Table.ColumnHeader>Trigger</Table.ColumnHeader>
                <Table.ColumnHeader>Requested / duration</Table.ColumnHeader>
                <Table.ColumnHeader>Action</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {runs.map((run) => (
                <Table.Row key={run.id}>
                  <Table.Cell>{identity(run)}</Table.Cell>
                  {!hideTarget && <Table.Cell>{target(run)}</Table.Cell>}
                  <Table.Cell>{outcome(run)}</Table.Cell>
                  <Table.Cell><Text fontSize="sm">{countsLabel(run.counts)}</Text></Table.Cell>
                  <Table.Cell>{runTriggerLabel(run)}</Table.Cell>
                  <Table.Cell>
                    <Stack gap="0.5">
                      <Text>{formatDateTime(run.requestedAt)}</Text>
                      <Text fontSize="xs" color="fg.muted">{runDuration(run.startedAt, run.finishedAt)}</Text>
                    </Stack>
                  </Table.Cell>
                  <Table.Cell>{action(run)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label={label}>
          {runs.map((run) => (
            <ResourceCard key={run.id} title={identity(run)} status={outcome(run)} actions={action(run)}>
              {!hideTarget && <ResourceCardField label="Target">{target(run)}</ResourceCardField>}
              <ResourceCardField label="Findings">{countsLabel(run.counts)}</ResourceCardField>
              <ResourceCardField label="Trigger">{runTriggerLabel(run)}</ResourceCardField>
              <ResourceCardField label="Requested">{formatDateTime(run.requestedAt)}</ResourceCardField>
              <ResourceCardField label="Duration">{runDuration(run.startedAt, run.finishedAt)}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
