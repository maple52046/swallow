import { useEffect, useMemo, useState } from 'react'
import { Button, HStack, Text } from '@chakra-ui/react'
import { ExternalLink } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isMetricRangeWindow, METRIC_DESCRIPTORS, type MetricRangeWindow, type MetricScope } from '@/domain/monitoring/types'
import { UsageCharts } from '@/presentation/components/monitoring/UsageCharts'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useMetricRange } from '@/presentation/hooks/useMetricRange'

/** What the console can chart: an explicit Server selection or one grouping. */
type ChartScopeKind = MetricScope['kind']

/** A grouping the operator can pick, loaded for the current Site. */
interface ScopeOption {
  id: string
  name: string
}

/** Zones, Pools, and Platforms of the Site, or the failure that prevented listing them. */
type ScopeOptionsState =
  | { status: 'loading' }
  | { status: 'ready'; zone: ScopeOption[]; pool: ScopeOption[]; platform: ScopeOption[] }
  | { status: 'error'; message: string }

/** A settled listing, remembered with the Site it belongs to so another Site reads as loading. */
interface SettledOptions {
  siteKey: string
  state: Exclude<ScopeOptionsState, { status: 'loading' }>
}

const SCOPE_KIND_LABEL: Record<ChartScopeKind, string> = {
  servers: 'Selected servers',
  pool: 'Pool',
  zone: 'Zone',
  platform: 'Platform',
}

/** Narrows the URL's `chartScope`; anything unknown falls back to the Server selection. */
function parseScopeKind(value: string | null): ChartScopeKind {
  return value === 'pool' || value === 'zone' || value === 'platform' ? value : 'servers'
}

const HOST_DESCRIPTORS = METRIC_DESCRIPTORS.filter((descriptor) => !descriptor.gpuOnly)
const ALL_METRICS = METRIC_DESCRIPTORS.map((descriptor) => descriptor.name)

interface FleetUsageChartsProps {
  /** Servers ticked in the console's Server metrics table (URL `chartServers`), in tick order. */
  selectedServerIds: readonly string[]
  /** Clears the Server selection, owned by the page because the table renders the checkboxes. */
  onClearSelection: () => void
  /** Bumped by the page's Refresh action to reload the charts with everything else. */
  refreshKey: number
}

/**
 * Monitoring console section that charts aggregated usage for the selected Servers, a Pool, a
 * Zone, or a Platform of the current Site.
 *
 * Scope and window are URL-owned (`chartScope`, `chartScopeId`, `chartWindow`) so a link
 * reproduces the view — Platform detail links here with a Platform preselected. The backend
 * resolves a grouping to its current members and aggregates each metric (average for
 * utilisation, sum for throughput and power, maximum for temperature). GPU cards appear only when
 * the scope returned GPU data, so a CPU-only Pool is not padded with "No data" GPU cards.
 * Grouping lists are loaded per Site; a listing failure is shown inline and leaves the Server
 * selection usable.
 */
export function FleetUsageCharts({ selectedServerIds, onClearSelection, refreshKey }: FleetUsageChartsProps) {
  const { infrastructure, platforms } = useApp()
  const { siteId, loading: siteScopeLoading, scopedHref } = useSiteScope()
  const [searchParams, setSearchParams] = useSearchParams()
  const [settledOptions, setSettledOptions] = useState<SettledOptions | null>(null)
  const siteKey = siteId ?? ''
  const options: ScopeOptionsState = !siteScopeLoading && settledOptions?.siteKey === siteKey ? settledOptions.state : { status: 'loading' }
  const kind = parseScopeKind(searchParams.get('chartScope'))
  const scopeId = searchParams.get('chartScopeId') ?? ''
  const requestedWindow = searchParams.get('chartWindow')
  const window: MetricRangeWindow = isMetricRangeWindow(requestedWindow) ? requestedWindow : '1h'

  // Lists the Site's groupings once per Site. Answers are stored with their Site key and the
  // cleanup marks a superseded request stale, so a slow answer for the previous Site never fills
  // the pickers.
  useEffect(() => {
    if (siteScopeLoading) return
    let cancelled = false
    Promise.all([infrastructure.listGroups('zone', siteId), infrastructure.listGroups('pool', siteId), platforms.listPlatforms(siteId)])
      .then(([zones, pools, platformList]) => {
        if (cancelled) return
        setSettledOptions({
          siteKey,
          state: {
            status: 'ready',
            zone: zones.map((zone) => ({ id: zone.id, name: zone.spec.name })),
            pool: pools.map((pool) => ({ id: pool.id, name: pool.spec.name })),
            platform: platformList.map((platform) => ({ id: platform.id, name: platform.name })),
          },
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setSettledOptions({ siteKey, state: { status: 'error', message: error instanceof Error ? error.message : 'Groupings could not be listed.' } })
      })
    return () => {
      cancelled = true
    }
  }, [infrastructure, platforms, siteId, siteKey, siteScopeLoading])

  const scope = useMemo<MetricScope | null>(() => {
    if (kind === 'servers') return { kind: 'servers', serverIds: [...selectedServerIds] }
    return scopeId ? { kind, id: scopeId } : null
  }, [kind, scopeId, selectedServerIds])
  const usage = useMetricRange(scope, ALL_METRICS, window, refreshKey)

  const descriptors = useMemo(() => {
    if (usage.status !== 'ready') return HOST_DESCRIPTORS
    const withData = new Set(usage.result.series.map((series) => series.metric))
    return METRIC_DESCRIPTORS.filter((descriptor) => !descriptor.gpuOnly || withData.has(descriptor.name))
  }, [usage])

  const updateParams = (apply: (params: URLSearchParams) => void) => {
    const next = new URLSearchParams(searchParams)
    apply(next)
    setSearchParams(next, { replace: true })
  }
  const changeKind = (value: string) => updateParams((params) => {
    const nextKind = parseScopeKind(value)
    if (nextKind === 'servers') params.delete('chartScope')
    else params.set('chartScope', nextKind)
    params.delete('chartScopeId')
  })
  const changeScopeId = (value: string) => updateParams((params) => {
    if (value) params.set('chartScopeId', value)
    else params.delete('chartScopeId')
  })
  const changeWindow = (next: MetricRangeWindow) => updateParams((params) => {
    if (next === '1h') params.delete('chartWindow')
    else params.set('chartWindow', next)
  })

  const groupOptions = kind !== 'servers' && options.status === 'ready' ? options[kind] : []
  const grafana = usage.status === 'ready' ? usage.result.grafana : null
  const idleMessage = kind === 'servers'
    ? 'Tick Servers in the Server metrics table below to chart them together, or chart a Pool, Zone, or Platform.'
    : `Choose a ${SCOPE_KIND_LABEL[kind].toLowerCase()} to chart its current members.`

  const toolbar = (
    <HStack gap="3" wrap="wrap" align="center">
      <Select
        value={kind}
        onChange={changeKind}
        aria-label="Chart scope"
        size="sm"
        width="11rem"
        options={(Object.keys(SCOPE_KIND_LABEL) as ChartScopeKind[]).map((value) => ({ value, label: SCOPE_KIND_LABEL[value] }))}
      />
      {kind === 'servers' ? (
        <HStack gap="2">
          <Text fontSize="sm" color="fg.muted" role="status">
            {selectedServerIds.length === 0 ? 'No servers selected' : `${selectedServerIds.length} selected`}
          </Text>
          {selectedServerIds.length > 0 && (
            <Button variant="plain" size="sm" onClick={onClearSelection}>Clear selection</Button>
          )}
        </HStack>
      ) : (
        <Select
          value={scopeId}
          onChange={changeScopeId}
          aria-label={`Chart ${SCOPE_KIND_LABEL[kind].toLowerCase()}`}
          placeholder={options.status === 'loading' ? 'Loading…' : groupOptions.length === 0 ? `No ${SCOPE_KIND_LABEL[kind].toLowerCase()}s in this Site` : `Choose a ${SCOPE_KIND_LABEL[kind].toLowerCase()}`}
          disabled={options.status !== 'ready' || groupOptions.length === 0}
          size="sm"
          width="14rem"
          options={groupOptions.map((option) => ({ value: option.id, label: option.name }))}
        />
      )}
      {options.status === 'error' && kind !== 'servers' && (
        <Text fontSize="sm" color="fg.error" role="alert">{`Groupings are unavailable: ${options.message}`}</Text>
      )}
    </HStack>
  )

  return (
    <UsageCharts
      title="Usage charts"
      description="Aggregated usage over time for the chosen Servers or grouping."
      state={usage}
      descriptors={descriptors}
      window={window}
      onWindowChange={changeWindow}
      idleMessage={idleMessage}
      integrationsHref={scopedHref('/infrastructure/integrations')}
      toolbar={toolbar}
      actions={grafana ? (
        <Button asChild variant="ghost" size="xs">
          <a href={grafana} target="_blank" rel="noreferrer">
            Explore in Grafana
            <ExternalLink size={14} />
          </a>
        </Button>
      ) : undefined}
    />
  )
}
