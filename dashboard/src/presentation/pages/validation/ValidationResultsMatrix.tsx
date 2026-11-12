import { useState } from 'react'
import { Badge, Box, Button, HStack, Stack, Table, Text } from '@chakra-ui/react'
import type { ValidationResult, ValidationResultTarget, ValidationRun } from '@/domain/validation/types'
import { KeyValueGrid, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { resultMatrix, VERDICT_BADGE, VERDICT_LABEL } from '@/presentation/components/validation/validationPresentation'
import { formatDateTime } from '@/shared/utils/time'

interface ValidationResultsMatrixProps {
  run: ValidationRun
  /** The display name of a result target: a Server's name or the Platform's. */
  targetName: (target: ValidationResultTarget) => string
}

/**
 * A run's results as a check × target matrix (`resultMatrix`): one row per check of the suite, one
 * column per target a result judges, each cell the check's verdict on that target as a labelled
 * badge. Choosing a cell shows that result below the matrix: its summary, measurements, the
 * evidence the host printed (bounded and redacted by the API), and the Task that judged it, whose
 * log is on the run's Workflow page.
 *
 * A cell with no result yet reads "No result": while the run executes that is pending work, and
 * once it completes the API has recorded an `error` in its place, so the dashboard never infers a
 * verdict. Gating checks — the ones a deployment's gate stops on — are marked as such. Below the
 * `md` breakpoint each check lists its targets instead of a wide table.
 */
export function ValidationResultsMatrix({ run, targetName }: ValidationResultsMatrixProps) {
  const { columns, rows } = resultMatrix(run)
  const [selected, setSelected] = useState<ValidationResult | null>(null)
  const current = selected
    ? run.results.find((result) => result.check === selected.check && result.target.kind === selected.target.kind && result.target.id === selected.target.id) ?? null
    : null
  const checkTitle = (id: string) => run.checks.find((check) => check.id === id)?.title ?? id

  const cell = (result: ValidationResult | null, check: string, target: ValidationResultTarget) => {
    if (!result) return <Text fontSize="sm" color="fg.muted">No result</Text>
    const chosen = current === result
    return (
      <Button
        size="xs"
        variant={chosen ? 'subtle' : 'ghost'}
        aria-pressed={chosen}
        aria-label={`${checkTitle(check)} on ${targetName(target)}: ${VERDICT_LABEL[result.verdict]}. Show details`}
        onClick={() => setSelected(chosen ? null : result)}
      >
        <StatusBadge status={VERDICT_BADGE[result.verdict]} label={VERDICT_LABEL[result.verdict]} />
      </Button>
    )
  }
  const checkHeading = (row: (typeof rows)[number]) => (
    <Stack gap="0.5">
      <HStack gap="2">
        <Text fontWeight="medium">{row.check.title}</Text>
        {row.check.gating && <Badge size="sm" variant="outline">Gating</Badge>}
      </HStack>
      {row.check.description && <Text fontSize="xs" color="fg.muted">{row.check.description}</Text>}
    </Stack>
  )

  return (
    <Stack gap="4">
      <ResponsiveDataView
        desktop={
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Results by check and target">
              <Table.Header>
                <Table.Row>
                  <Table.ColumnHeader>Check</Table.ColumnHeader>
                  {columns.map((column) => <Table.ColumnHeader key={column.key}>{targetName(column.target)}</Table.ColumnHeader>)}
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((row) => (
                  <Table.Row key={row.check.id}>
                    <Table.Cell>{checkHeading(row)}</Table.Cell>
                    {row.cells.map((result, index) => (
                      <Table.Cell key={columns[index].key}>{cell(result, row.check.id, columns[index].target)}</Table.Cell>
                    ))}
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        }
        mobile={
          <Stack gap="3" aria-label="Results by check and target" role="list">
            {rows.map((row) => (
              <Box key={row.check.id} role="listitem" borderWidth="1px" rounded="lg" p="3">
                {checkHeading(row)}
                <Stack gap="1" mt="2">
                  {row.cells.map((result, index) => (
                    <HStack key={columns[index].key} justify="space-between">
                      <Text fontSize="sm">{targetName(columns[index].target)}</Text>
                      {cell(result, row.check.id, columns[index].target)}
                    </HStack>
                  ))}
                </Stack>
              </Box>
            ))}
          </Stack>
        }
      />
      <Box aria-live="polite">
        {current && <ResultDetails result={current} checkTitle={checkTitle(current.check)} targetName={targetName(current.target)} />}
      </Box>
    </Stack>
  )
}

/** One result in full: what was found, what was measured against what was expected, and the evidence. */
function ResultDetails({ result, checkTitle, targetName }: { result: ValidationResult; checkTitle: string; targetName: string }) {
  return (
    <Stack gap="3" borderWidth="1px" rounded="lg" p="4" aria-label={`${checkTitle} on ${targetName}`} role="region">
      <HStack gap="3" wrap="wrap">
        <Text fontWeight="semibold">{checkTitle} on {targetName}</Text>
        <StatusBadge status={VERDICT_BADGE[result.verdict]} label={VERDICT_LABEL[result.verdict]} />
      </HStack>
      {result.summary && <Text>{result.summary}</Text>}
      <KeyValueGrid items={[
        { label: 'Observed', value: formatDateTime(result.observedAt) },
        { label: 'Task', value: result.taskId ?? 'Recorded by swallow: no Task judged it' },
      ]} />
      {result.measurements.length > 0 && (
        <Table.Root size="sm" aria-label="Measurements">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeader>Measurement</Table.ColumnHeader>
              <Table.ColumnHeader>Value</Table.ColumnHeader>
              <Table.ColumnHeader>Expected</Table.ColumnHeader>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {result.measurements.map((measurement) => (
              <Table.Row key={measurement.name}>
                <Table.Cell>{measurement.name}</Table.Cell>
                <Table.Cell>{measurement.value} {measurement.unit}</Table.Cell>
                <Table.Cell>{measurement.expected || '—'}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
      {result.evidence && (
        <Stack gap="1">
          <Text fontSize="sm" fontWeight="medium">Evidence</Text>
          <Box
            as="pre"
            className="sw-mono"
            fontSize="xs"
            whiteSpace="pre-wrap"
            wordBreak="break-word"
            maxH="64"
            overflowY="auto"
            bg="bg.muted"
            rounded="md"
            p="3"
            aria-label="Evidence"
            tabIndex={0}
          >
            {result.evidence}
          </Box>
        </Stack>
      )}
    </Stack>
  )
}
