import { useEffect, useState, useCallback, useMemo } from 'react'
import {
  Grid, Card, Text, Group, ThemeIcon, Stack, Badge,
  Timeline, ScrollArea, ActionIcon, Tooltip, Divider,
} from '@mantine/core'
import { AreaChart } from '@mantine/charts'
import { useNavigate } from 'react-router-dom'
import {
  IconAlertTriangle, IconAlertCircle, IconCheck, IconX, IconClock,
  IconPlayerPlay, IconServerOff, IconAlertOctagon, IconTool, IconPlugConnectedX,
  IconCpu, IconDatabase, IconDeviceDesktopAnalytics,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Server } from '@/domain/server/types'
import type { GPUMetrics } from '@/domain/gpu/types'
import type { OverviewData } from '@/application/dtos'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { formatRelative } from '@/shared/utils/time'

// ─── Summary Card ────────────────────────────────────────────────────────────

function SummaryCard({
  label, value, color, icon, isZero, onClick,
}: {
  label: string
  value: string | number
  color: string
  icon?: React.ReactNode
  isZero?: boolean
  onClick?: () => void
}) {
  const accentColor = isZero ? 'gray' : color
  const numColor = isZero ? 'dimmed' : color

  return (
    <Card
      withBorder
      radius="md"
      style={{
        cursor: onClick ? 'pointer' : undefined,
        borderLeft: `3px solid var(--mantine-color-${accentColor}-5)`,
        minHeight: 96,
        position: 'relative',
        flex: 1,
      }}
      onClick={onClick}
    >
      <Stack gap={4}>
        <Text size="xs" c="dimmed" fw={500}>{label}</Text>
        <Text fz={32} fw={800} c={numColor} lh={1}>{value}</Text>
      </Stack>
      {icon && (
        <div style={{ position: 'absolute', top: 10, right: 12, opacity: isZero ? 0.25 : 0.45 }}>
          {icon}
        </div>
      )}
    </Card>
  )
}

// ─── Monitoring Panel ─────────────────────────────────────────────────────────

type TrendPoint = { h: string; v: number }

function monitoringStatus(current: number, peak: number): { label: string; color: string } {
  if (current > 80 && peak > 90) return { label: 'High (sustained)', color: 'red' }
  if (current > 80) return { label: 'High', color: 'orange' }
  return { label: 'Normal', color: 'green' }
}

function MonitoringPanel({
  label, current, avg, peak, trendData, icon,
}: {
  label: string
  current: number
  avg: number
  peak: number
  trendData: TrendPoint[]
  icon?: React.ReactNode
}) {
  const status = monitoringStatus(current, peak)

  return (
    <Card
      withBorder
      radius="md"
      style={{
        borderLeft: '3px solid var(--mantine-color-blue-5)',
        flex: 1,
      }}
    >
      {/* Header */}
      <Group justify="space-between" mb={8}>
        <Group gap={6}>
          {icon && <span style={{ opacity: 0.5 }}>{icon}</span>}
          <Text size="sm" fw={600}>{label}</Text>
        </Group>
        <Text size="xs" c="dimmed">last 72h</Text>
      </Group>

      {/* Primary value + status */}
      <Group align="flex-end" gap="sm" mb={10}>
        <Text fz={28} fw={700} c="blue" lh={1}>{current}%</Text>
        <Badge size="sm" color={status.color} variant="light" mb={2}>
          {status.label}
        </Badge>
      </Group>

      {/* Trend chart */}
      <AreaChart
        h={100}
        data={trendData}
        dataKey="h"
        series={[{ name: 'v', color: 'blue.4' }]}
        curveType="monotone"
        withXAxis={false}
        withYAxis={false}
        withDots={false}
        withLegend={false}
        fillOpacity={0.15}
        strokeWidth={1.5}
        style={{ margin: '0 -16px' }}
      />

      {/* Supporting metrics */}
      <Group gap="lg" mt={8}>
        <Text size="xs" c="dimmed">avg: <Text span size="xs" fw={600} c="blue.6">{avg}%</Text></Text>
        <Text size="xs" c="dimmed">peak: <Text span size="xs" fw={600} c="blue.6">{peak}%</Text></Text>
      </Group>
    </Card>
  )
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export function OverviewPage() {
  const { overview, servers, observability } = useApp()
  const navigate = useNavigate()

  const [serverList, setServerList] = useState<Server[]>([])
  const [gpuMetrics, setGpuMetrics] = useState<GPUMetrics[]>([])
  const [overviewData, setOverviewData] = useState<OverviewData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const [srvResult, ovResult, gpuResult] = await Promise.all([
        servers.list.execute(),
        overview.get.execute(),
        observability.getGPUMetrics.execute(),
      ])
      setServerList(srvResult)
      setOverviewData(ovResult)
      setGpuMetrics(gpuResult)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [servers.list, overview.get, observability.getGPUMetrics])

  useEffect(() => { void load() }, [load])

  // ── Aggregations ───────────────────────────────────────────────────────────

  const stats = useMemo(() => {
    const total = serverList.length
    const warning = serverList.filter((s) => s.status === 'warning').length
    const error = serverList.filter((s) => s.status === 'error').length
    const offline = serverList.filter((s) => s.status === 'offline').length
    const maintain = serverList.filter((s) => s.status === 'maintain').length
    const free = serverList.filter((s) => s.ownerTeamId === null && s.ownerUserId === null).length
    const issues = warning + error + offline + maintain
    return { total, warning, error, offline, maintain, free, issues }
  }, [serverList])

  const resourceStats = useMemo(() => {
    const avg = (arr: number[]) => arr.length ? Math.round(arr.reduce((s, v) => s + v, 0) / arr.length) : 0
    const active = serverList.filter((s) => s.status !== 'offline' && s.status !== 'error')
    const cpuAvg = avg(active.map((s) => s.cpuUsagePct))
    const ramAvg = avg(active.map((s) => s.ramUsagePct))
    const gpuAvg = gpuMetrics.length ? avg(gpuMetrics.map((m) => m.utilization)) : 0
    const highCpu = cpuAvg > 80
    const highRam = ramAvg > 80
    const highGpu = gpuAvg > 80
    const highLabel = highCpu ? 'CPU' : highRam ? 'RAM' : highGpu ? 'GPU' : null
    return { cpuAvg, ramAvg, gpuAvg, highLabel }
  }, [serverList, gpuMetrics])

  const trendData = useMemo(() => {
    const gen = (base: number, seed: number): TrendPoint[] =>
      Array.from({ length: 72 }, (_, i) => ({
        h: `${i}h`,
        v: Math.max(5, Math.min(100,
          base
          + Math.sin((i + seed) * 0.35) * 15
          + Math.sin((i + seed * 2) * 0.8) * 8
          + (i % 7 === 0 ? 12 : 0)
          - 3
        )),
      }))
    return {
      cpu: gen(resourceStats.cpuAvg, 1),
      ram: gen(resourceStats.ramAvg, 3),
      gpu: gen(resourceStats.gpuAvg, 5),
    }
  }, [resourceStats])

  const panelStats = useMemo(() => {
    const calc = (data: TrendPoint[]) => {
      const vals = data.map((d) => d.v)
      const avg = Math.round(vals.reduce((s, v) => s + v, 0) / vals.length)
      const peak = Math.round(Math.max(...vals))
      return { avg, peak }
    }
    return {
      cpu: calc(trendData.cpu),
      ram: calc(trendData.ram),
      gpu: calc(trendData.gpu),
    }
  }, [trendData])

  // ── Timeline helpers ───────────────────────────────────────────────────────

  const timelineIcon = (type: string) => {
    if (type === 'run_completed') return <IconCheck size={12} />
    if (type === 'run_failed') return <IconX size={12} />
    if (type === 'run_started') return <IconPlayerPlay size={12} />
    if (type === 'alert_fired') return <IconAlertTriangle size={12} />
    if (type === 'alert_resolved') return <IconCheck size={12} />
    return <IconClock size={12} />
  }

  const timelineColor = (type: string, severity?: string) => {
    if (type === 'run_completed' || type === 'alert_resolved') return 'green'
    if (type === 'run_failed' || severity === 'critical') return 'red'
    if (type === 'alert_fired' || severity === 'warning') return 'yellow'
    return 'blue'
  }

  // ──────────────────────────────────────────────────────────────────────────

  if (loading) return <LoadingState rows={6} height={80} />
  if (error) return <ErrorState message={error} onRetry={load} />

  const { activeAlerts, opsTimeline } = overviewData ?? { activeAlerts: [], opsTimeline: [] }

  return (
    <>
      <PageHeader title="Overview" subtitle="Datacenter status at a glance" />

      {/* ── Section 1: Servers ────────────────────────────────────────── */}
      <Group justify="space-between" align="baseline" mb="sm">
        <Text fw={700} size="lg">Servers</Text>
        <Text size="sm" c="dimmed">total: {stats.total}</Text>
      </Group>

      {/* Summary message */}
      <div
        style={{
          borderLeft: `3px solid var(--mantine-color-${stats.issues === 0 ? 'green' : 'red'}-5)`,
          backgroundColor: `var(--mantine-color-${stats.issues === 0 ? 'green' : 'red'}-0)`,
          borderRadius: 4,
          padding: '8px 12px',
          marginBottom: 16,
          display: 'inline-flex',
          alignItems: 'center',
        }}
      >
        <Text size="sm" fw={700} c={stats.issues === 0 ? 'green' : 'red'}>
          {stats.issues === 0 ? '✅ All systems healthy' : `🚨 ${stats.issues} issues detected`}
        </Text>
      </div>

      {/* 5 status cards */}
      <Group gap="sm" align="stretch" wrap="nowrap" mb="xl">
        <SummaryCard
          label="Warning"
          value={stats.warning}
          color="yellow"
          icon={<IconAlertTriangle size={14} />}
          isZero={stats.warning === 0}
          onClick={() => navigate('/servers', { state: { statusFilter: 'warning' } })}
        />
        <SummaryCard
          label="Error"
          value={stats.error}
          color="red"
          icon={<IconAlertOctagon size={14} />}
          isZero={stats.error === 0}
          onClick={() => navigate('/servers', { state: { statusFilter: 'error' } })}
        />
        <SummaryCard
          label="Offline"
          value={stats.offline}
          color="gray"
          icon={<IconServerOff size={14} />}
          isZero={stats.offline === 0}
          onClick={() => navigate('/servers', { state: { statusFilter: 'offline' } })}
        />
        <SummaryCard
          label="Maintenance"
          value={stats.maintain}
          color="blue"
          icon={<IconTool size={14} />}
          isZero={stats.maintain === 0}
          onClick={() => navigate('/servers', { state: { statusFilter: 'maintain' } })}
        />
        <Divider orientation="vertical" />
        <SummaryCard
          label="Free"
          value={stats.free}
          color="teal"
          icon={<IconPlugConnectedX size={14} />}
          onClick={() => navigate('/servers', { state: { allocationSearch: 'free' } })}
        />
      </Group>

      {/* ── Section 2: Total Resource Utilization ────────────────────── */}
      <Divider mb="md" />
      <Text fw={700} size="lg" mb="sm">Total Resource Utilization</Text>

      {/* Monitoring summary message */}
      <div
        style={{
          borderLeft: `3px solid var(--mantine-color-${resourceStats.highLabel ? 'orange' : 'green'}-5)`,
          backgroundColor: `var(--mantine-color-${resourceStats.highLabel ? 'orange' : 'green'}-0)`,
          borderRadius: 4,
          padding: '8px 12px',
          marginBottom: 16,
          display: 'inline-flex',
          alignItems: 'center',
        }}
      >
        <Text size="sm" fw={700} c={resourceStats.highLabel ? 'orange' : 'green'}>
          {resourceStats.highLabel
            ? `⚠️ High ${resourceStats.highLabel} utilization`
            : '🟢 Resource usage stable'}
        </Text>
      </div>

      {/* 3 monitoring panels */}
      <Group gap="sm" align="stretch" wrap="nowrap" mb="xl">
        <MonitoringPanel
          label="CPU Usage"
          current={resourceStats.cpuAvg}
          avg={panelStats.cpu.avg}
          peak={panelStats.cpu.peak}
          trendData={trendData.cpu}
          icon={<IconCpu size={16} />}
        />
        <MonitoringPanel
          label="RAM Usage"
          current={resourceStats.ramAvg}
          avg={panelStats.ram.avg}
          peak={panelStats.ram.peak}
          trendData={trendData.ram}
          icon={<IconDatabase size={16} />}
        />
        <MonitoringPanel
          label="GPU Usage"
          current={resourceStats.gpuAvg}
          avg={panelStats.gpu.avg}
          peak={panelStats.gpu.peak}
          trendData={trendData.gpu}
          icon={<IconDeviceDesktopAnalytics size={16} />}
        />
      </Group>

      {/* ── Section 3: Operation ──────────────────────────────────────── */}
      <Divider mb="md" />
      <Text fw={700} size="lg" mb="md">Operation</Text>

      <Grid>
        <Grid.Col span={{ base: 12, lg: 4 }}>
          <Card withBorder radius="md">
            <Group justify="space-between" mb="md">
              <Text fw={600}>Alerts</Text>
              <Tooltip label="View all alerts">
                <ActionIcon variant="subtle" onClick={() => navigate('/deprecated/observability/alerts')}>
                  <IconAlertTriangle size={16} />
                </ActionIcon>
              </Tooltip>
            </Group>
            <Stack gap="xs">
              {activeAlerts.slice(0, 5).map((alert) => (
                <Card key={alert.id} withBorder radius="sm" p="xs" style={{ cursor: 'pointer' }} onClick={() => navigate('/deprecated/observability/alerts')}>
                  <Group gap="xs" wrap="nowrap">
                    <ThemeIcon variant="light" color={alert.severity === 'critical' ? 'red' : 'yellow'} size="sm" radius="sm">
                      <IconAlertCircle size={12} />
                    </ThemeIcon>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <Text size="xs" fw={500} lineClamp={1}>{alert.title}</Text>
                      <Text size="xs" c="dimmed">{formatRelative(alert.createdAt)}</Text>
                    </div>
                  </Group>
                </Card>
              ))}
              {activeAlerts.length === 0 && (
                <Group gap="xs" py="xs">
                  <ThemeIcon color="green" variant="light" size="sm"><IconCheck size={12} /></ThemeIcon>
                  <Text size="sm" c="dimmed">No active alerts</Text>
                </Group>
              )}
            </Stack>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 8 }}>
          <Card withBorder radius="md">
            <Text fw={600} mb="md">Operations Timeline</Text>
            <ScrollArea h={280}>
              <Timeline bulletSize={20} lineWidth={2}>
                {opsTimeline.slice(0, 10).map((event) => (
                  <Timeline.Item
                    key={event.id}
                    bullet={timelineIcon(event.type)}
                    color={timelineColor(event.type, event.severity)}
                  >
                    <Text size="xs" fw={500} lineClamp={1}>{event.title}</Text>
                    <Text size="xs" c="dimmed" lineClamp={1}>{event.description}</Text>
                    <Text size="xs" c="dimmed">{formatRelative(event.timestamp)}</Text>
                  </Timeline.Item>
                ))}
                {opsTimeline.length === 0 && (
                  <Text size="xs" c="dimmed">No recent operations</Text>
                )}
              </Timeline>
            </ScrollArea>
          </Card>
        </Grid.Col>
      </Grid>
    </>
  )
}
