import { Fragment, useState } from 'react'
import { Badge, Box, Button, HStack, Stack, Table, Text } from '@chakra-ui/react'
import type { ValidationResult, ValidationResultTarget, ValidationRun } from '@/domain/validation/types'
import { KeyValueGrid, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { resultMatrix, VERDICT_BADGE, VERDICT_LABEL } from '@/presentation/components/validation/validationPresentation'
import { formatDateTime } from '@/shared/utils/time'

interface ValidationResultsMatrixProps {
  run: ValidationRun
  targetName: (target: ValidationResultTarget) => string
}

/**
 * Complete check-by-target audit matrix. Selecting a verdict expands its inspector directly after
 * that check rather than detaching the failure reason at the bottom of a wide matrix.
 */
export function ValidationResultsMatrix({ run, targetName }: ValidationResultsMatrixProps) {
  const { columns, rows } = resultMatrix(run)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const keyOf = (result: ValidationResult) => `${result.check}|${result.target.kind}:${result.target.id}`
  const selected = selectedKey ? run.results.find((result) => keyOf(result) === selectedKey) ?? null : null
  const checkTitle = (id: string) => run.checks.find((check) => check.id === id)?.title ?? id

  const cell = (result: ValidationResult | null, check: string, target: ValidationResultTarget) => {
    if (!result) return <Text fontSize="sm" color="fg.muted">No result</Text>
    const chosen = selectedKey === keyOf(result)
    return (
      <Button
        size="xs"
        variant={chosen ? 'subtle' : 'ghost'}
        aria-pressed={chosen}
        aria-label={`${checkTitle(check)} on ${targetName(target)}: ${VERDICT_LABEL[result.verdict]}. ${chosen ? 'Hide' : 'Show'} details`}
        onClick={() => setSelectedKey(chosen ? null : keyOf(result))}
      >
        <StatusBadge status={VERDICT_BADGE[result.verdict]} label={VERDICT_LABEL[result.verdict]} />
      </Button>
    )
  }
  const heading = (row: (typeof rows)[number]) => (
    <Stack gap="0.5">
      <HStack gap="2">
        <Text fontWeight="medium">{row.check.title}</Text>
        {row.check.gating && <Badge size="sm" variant="outline">Gating</Badge>}
      </HStack>
      {row.check.description && <Text fontSize="xs" color="fg.muted">{row.check.description}</Text>}
    </Stack>
  )

  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Results by check and target">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader position="sticky" left="0" zIndex="1" bg="bg.panel" minW="16rem">Check</Table.ColumnHeader>
                {columns.map((column) => <Table.ColumnHeader key={column.key} minW="10rem">{targetName(column.target)}</Table.ColumnHeader>)}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.map((row) => (
                <Fragment key={row.check.id}>
                  <Table.Row>
                    <Table.Cell position="sticky" left="0" zIndex="1" bg="bg.panel">{heading(row)}</Table.Cell>
                    {row.cells.map((result, index) => (
                      <Table.Cell key={columns[index].key}>{cell(result, row.check.id, columns[index].target)}</Table.Cell>
                    ))}
                  </Table.Row>
                  {selected?.check === row.check.id && (
                    <Table.Row>
                      <Table.Cell colSpan={columns.length + 1} p="3" bg="bg.subtle">
                        <ValidationResultInspector
                          result={selected}
                          checkTitle={checkTitle(selected.check)}
                          targetName={targetName(selected.target)}
                        />
                      </Table.Cell>
                    </Table.Row>
                  )}
                </Fragment>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <Stack gap="3" aria-label="Results by check and target" role="list">
          {rows.map((row) => (
            <Box key={row.check.id} role="listitem" borderWidth="1px" rounded="lg" p="3">
              {heading(row)}
              <Stack gap="1" mt="2">
                {row.cells.map((result, index) => (
                  <HStack key={columns[index].key} justify="space-between" align="flex-start">
                    <Text fontSize="sm">{targetName(columns[index].target)}</Text>
                    {cell(result, row.check.id, columns[index].target)}
                  </HStack>
                ))}
              </Stack>
              {selected?.check === row.check.id && (
                <Box mt="3">
                  <ValidationResultInspector result={selected} checkTitle={checkTitle(selected.check)} targetName={targetName(selected.target)} />
                </Box>
              )}
            </Box>
          ))}
        </Stack>
      }
    />
  )
}

/** Full API-backed reason, measurements, and evidence for one result. */
export function ValidationResultInspector({
  result,
  checkTitle,
  targetName,
}: {
  result: ValidationResult
  checkTitle: string
  targetName: string
}) {
  return (
    <Stack gap="3" borderWidth="1px" rounded="lg" p="4" bg="bg.panel" aria-label={`${checkTitle} on ${targetName}`} role="region">
      <HStack gap="3" wrap="wrap">
        <Text fontWeight="semibold">{checkTitle} on {targetName}</Text>
        <StatusBadge status={VERDICT_BADGE[result.verdict]} label={VERDICT_LABEL[result.verdict]} />
      </HStack>
      {result.summary && <Text>{result.summary}</Text>}
      <KeyValueGrid items={[
        { label: 'Observed', value: formatDateTime(result.observedAt) },
        { label: 'Task', value: result.taskId ?? 'Recorded by swallow: no Task completed this check' },
      ]} />
      {result.measurements.length > 0 && (
        <Table.Root size="sm" aria-label="Measurements">
          <Table.Header>
            <Table.Row><Table.ColumnHeader>Measurement</Table.ColumnHeader><Table.ColumnHeader>Value</Table.ColumnHeader><Table.ColumnHeader>Expected</Table.ColumnHeader></Table.Row>
          </Table.Header>
          <Table.Body>{result.measurements.map((measurement) => (
            <Table.Row key={measurement.name}>
              <Table.Cell>{measurement.name}</Table.Cell>
              <Table.Cell>{measurement.value} {measurement.unit}</Table.Cell>
              <Table.Cell>{measurement.expected || '—'}</Table.Cell>
            </Table.Row>
          ))}</Table.Body>
        </Table.Root>
      )}
      {result.evidence && (
        <Box as="details">
          <Box as="summary" cursor="pointer" fontSize="sm" fontWeight="medium">Evidence</Box>
          <Box as="pre" className="sw-mono" fontSize="xs" whiteSpace="pre-wrap" wordBreak="break-word" maxH="64" overflowY="auto" bg="bg.muted" rounded="md" p="3" mt="2" tabIndex={0}>
            {result.evidence}
          </Box>
        </Box>
      )}
    </Stack>
  )
}
