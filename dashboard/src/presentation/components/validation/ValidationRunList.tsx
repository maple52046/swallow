import { Link as ChakraLink, Stack, Table, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import type { ValidationRunSummary } from '@/domain/validation/types'
import { StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { formatDateTime } from '@/shared/utils/time'
import { countsLabel, runOutcome, runTriggerLabel } from './validationPresentation'

interface ValidationRunListProps {
  runs: readonly ValidationRunSummary[]
  /** Accessible name of the table and of the card list, for example "Validation Runs". */
  label: string
  /** Omits the Target column where every run has the same target (a Server's or Platform's page). */
  hideTarget?: boolean
}

/**
 * The one presentation of a list of Validation Runs: the Validation area's Runs list and the recent
 * runs on Server and Platform pages compose it. Each run links to its detail page (keeping the
 * Site scope) and shows its suite, target, what started it, and its outcome: the verdict of a
 * completed run, else its status, always as text with the counts of each verdict.
 *
 * Targets are shown by name (`useServerNames`, `usePlatformNamesById`); an id without a name (a
 * deleted Server) is shown as the id. Below the `md` breakpoint the same runs are cards.
 */
export function ValidationRunList({ runs, label, hideTarget = false }: ValidationRunListProps) {
  const { scopedHref } = useSiteScope()
  const serverIds = runs.flatMap((run) => (run.target.platformId ? [] : run.target.serverIds))
  const serverNames = useServerNames(serverIds)
  const platformNames = usePlatformNamesById(runs.flatMap((run) => (run.target.platformId ? [run.target.platformId] : [])))
  const targetLabel = (run: ValidationRunSummary) => {
    if (run.target.platformId) return `Platform ${platformNames.get(run.target.platformId) ?? run.target.platformId}`
    return run.target.serverIds.map((id) => serverNames.get(id) ?? id).join(', ')
  }
  const suiteLink = (run: ValidationRunSummary) => (
    <ChakraLink asChild fontWeight="medium" color="brand.fg">
      <RouterLink to={scopedHref(`/validation/runs/${encodeURIComponent(run.id)}`)}>{run.suite.name}</RouterLink>
    </ChakraLink>
  )
  const outcome = (run: ValidationRunSummary) => {
    const { status, label: text } = runOutcome(run)
    return <StatusBadge status={status} label={text} />
  }

  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label={label}>
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Suite</Table.ColumnHeader>
                {!hideTarget && <Table.ColumnHeader>Target</Table.ColumnHeader>}
                <Table.ColumnHeader>Started by</Table.ColumnHeader>
                <Table.ColumnHeader>Result</Table.ColumnHeader>
                <Table.ColumnHeader>Requested</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {runs.map((run) => (
                <Table.Row key={run.id}>
                  <Table.Cell>{suiteLink(run)}</Table.Cell>
                  {!hideTarget && <Table.Cell>{targetLabel(run)}</Table.Cell>}
                  <Table.Cell>{runTriggerLabel(run)}</Table.Cell>
                  <Table.Cell>
                    <Stack gap="1" align="flex-start">
                      {outcome(run)}
                      <Text fontSize="xs" color="fg.muted">{countsLabel(run.counts)}</Text>
                    </Stack>
                  </Table.Cell>
                  <Table.Cell>{formatDateTime(run.requestedAt)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label={label}>
          {runs.map((run) => (
            <ResourceCard key={run.id} title={suiteLink(run)} status={outcome(run)}>
              {!hideTarget && <ResourceCardField label="Target">{targetLabel(run)}</ResourceCardField>}
              <ResourceCardField label="Started by">{runTriggerLabel(run)}</ResourceCardField>
              <ResourceCardField label="Results">{countsLabel(run.counts)}</ResourceCardField>
              <ResourceCardField label="Requested">{formatDateTime(run.requestedAt)}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
