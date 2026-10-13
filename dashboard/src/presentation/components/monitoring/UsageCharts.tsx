import { useId, type ReactNode } from 'react'
import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { Link } from 'react-router-dom'
import {
  METRIC_RANGE_WINDOWS,
  type MetricDescriptor,
  type MetricRangeResult,
  type MetricRangeWindow,
} from '@/domain/monitoring/types'
import { TimeSeriesChart } from '@/presentation/components/charts/TimeSeriesChart'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import type { MetricRangeState } from '@/presentation/hooks/useMetricRange'

interface UsageChartsProps {
  title?: string
  description?: string
  /** State from `useMetricRange` for the scope this section charts. */
  state: MetricRangeState
  /** Metrics to chart, in display order; each gets one card. */
  descriptors: readonly MetricDescriptor[]
  window: MetricRangeWindow
  onWindowChange: (window: MetricRangeWindow) => void
  /** Copy for the `idle` state, telling the operator how to choose a scope. */
  idleMessage?: string
  /** Where an operator registers a metrics Integration; shown when the backend is unavailable. */
  integrationsHref?: string
  /** Extra header controls, e.g. the optional Grafana link. */
  actions?: ReactNode
  /** Controls rendered above the charts, e.g. the Monitoring console's scope pickers. */
  toolbar?: ReactNode
}

/**
 * swallow's built-in usage charts: one card per named metric over a chosen window.
 *
 * Shared by the Server detail Monitoring tab (one Server) and the Monitoring console (selected
 * Servers, a Pool, a Zone, or a Platform, aggregated by the backend), so the two never drift into
 * different chart cards. Consumers own the scope and window state; this component only renders.
 *
 * State contract: `idle` explains how to pick a scope; `loading` keeps chart-sized skeletons;
 * `unavailable` explains that a metrics Integration is required (never "servers down");
 * `error` shows the backend message; an empty scope says so; a metric without data shows
 * "No data" in its card rather than a flat zero line. The window control is a labelled button
 * group whose pressed state is exposed through `aria-pressed`.
 */
export function UsageCharts({
  title = 'Usage history',
  description,
  state,
  descriptors,
  window,
  onWindowChange,
  idleMessage = 'Choose what to chart.',
  integrationsHref,
  actions,
  toolbar,
}: UsageChartsProps) {
  const windowPicker = (
    <HStack gap="1" role="group" aria-label="Time window" className="sw-window-picker">
      {METRIC_RANGE_WINDOWS.map((option) => (
        <Button
          key={option.value}
          size="xs"
          variant={option.value === window ? 'solid' : 'outline'}
          colorPalette={option.value === window ? 'brand' : undefined}
          aria-pressed={option.value === window}
          // The name keeps the visible "6h" first so voice control matches it, then spells it out.
          aria-label={`${option.value} (last ${option.label})`}
          onClick={() => onWindowChange(option.value)}
        >
          {option.value}
        </Button>
      ))}
    </HStack>
  )

  return (
    <SectionSurface
      title={title}
      description={description}
      actions={<HStack gap="2" wrap="wrap" justify="flex-end">{actions}{windowPicker}</HStack>}
      className="sw-usage-charts"
    >
      <Stack gap="4">
        {toolbar}
        <UsageChartsBody state={state} descriptors={descriptors} idleMessage={idleMessage} integrationsHref={integrationsHref} />
      </Stack>
    </SectionSurface>
  )
}

/** The state-dependent body of `UsageCharts`, kept separate so each state reads as one branch. */
function UsageChartsBody({
  state,
  descriptors,
  idleMessage,
  integrationsHref,
}: {
  state: MetricRangeState
  descriptors: readonly MetricDescriptor[]
  idleMessage: string
  integrationsHref?: string
}) {
  switch (state.status) {
    case 'idle':
      return <EmptyState title="Nothing to chart yet" message={idleMessage} />
    case 'loading':
      return <LoadingState rows={2} height={180} />
    case 'unavailable':
      return (
        <Alert status="info" title="No metrics backend for this Site">
          {state.message}{' '}
          {integrationsHref && <Link to={integrationsHref}>Open Integrations</Link>}
        </Alert>
      )
    case 'error':
      return <Alert status="warning" title="Usage history is unavailable">{state.message}</Alert>
    case 'ready':
      if (state.result.scope.serverCount === 0) {
        return <EmptyState title="No Servers in this scope" message="This grouping currently contains no Servers, so there is nothing to chart." />
      }
      return (
        <div className="sw-usage-chart-grid">
          {descriptors.map((descriptor) => (
            <UsageChartCard key={descriptor.name} descriptor={descriptor} result={state.result} />
          ))}
        </div>
      )
  }
}

/**
 * One metric's card: its latest value and its chart, or "No data" when the backend returned no
 * series. In aggregate mode there is one series; per-Server results draw one line each. The card
 * is a named group rather than a landmark region, so a page of charts does not flood the
 * landmark list.
 */
function UsageChartCard({ descriptor, result }: { descriptor: MetricDescriptor; result: MetricRangeResult }) {
  const headingId = useId()
  const series = result.series.filter((item) => item.metric === descriptor.name)
  const primary = series[0]?.points
  const latest = primary && primary.length > 0 ? primary[primary.length - 1].value : undefined
  const peak = primary && primary.length > 0 ? Math.max(...primary.map((point) => point.value)) : undefined
  return (
    <div className="sw-usage-chart-card" role="group" aria-labelledby={headingId}>
      <header className="sw-usage-chart-card__header">
        <Text as="h3" id={headingId} fontSize="sm" fontWeight="semibold" color="fg.muted">{descriptor.label}</Text>
        {series.length === 1 && latest !== undefined && (
          <Text as="p" className="sw-usage-chart-card__value">
            {descriptor.format(latest)}
            {peak !== undefined && <Text as="span" color="fg.muted" fontSize="xs" fontWeight="normal">{` peak ${descriptor.format(peak)}`}</Text>}
          </Text>
        )}
      </header>
      {series.length === 0 ? (
        <Text className="sw-usage-chart-card__empty" color="fg.muted">No data</Text>
      ) : (
        <TimeSeriesChart
          series={series.map((item) => ({ id: item.serverId ?? item.metric, label: item.serverId ?? descriptor.label, points: item.points }))}
          start={result.start}
          end={result.end}
          stepSeconds={result.stepSeconds}
          unit={descriptor.unit}
          format={descriptor.format}
          label={descriptor.label}
        />
      )}
    </div>
  )
}
