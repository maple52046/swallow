import { useEffect, useState, useCallback } from 'react'
import { SimpleGrid, Card, Text, Group, Badge, Stack, Button, Tabs } from '@mantine/core'
import { AreaChart, BarChart, DonutChart } from '@mantine/charts'
import { IconChartArea, IconActivity, IconCpu } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { GPUDevice, GPUMetrics } from '@/domain/gpu/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'

type GPUWithMetrics = GPUDevice & { metrics: GPUMetrics }

export function DashboardsPage() {
  const { observability, overview } = useApp()

  const [gpus, setGpus] = useState<GPUWithMetrics[]>([])
  const [overviewData, setOverviewData] = useState<Awaited<ReturnType<typeof overview.get.execute>> | null>(null)
  const [loading, setLoading] = useState(true)
  const [history, setHistory] = useState<{ time: string; avgUtil: number; avgTemp: number; critical: number }[]>([])

  const load = useCallback(async () => {
    const [gpuResult, metricsList, ov] = await Promise.all([
      observability.listGPUDevices.execute({}),
      observability.getGPUMetrics.execute(),
      overview.get.execute(),
    ])
    const metricsById = new Map(metricsList.map((m) => [m.gpuId, m]))
    const merged: GPUWithMetrics[] = gpuResult.map((gpu) => ({
      ...gpu,
      metrics: metricsById.get(gpu.id) ?? {
        gpuId: gpu.id, utilization: 0, memoryUsedMB: 0, memoryTotalMB: gpu.memoryGB * 1024,
        temperatureC: 0, powerDrawW: 0, powerLimitW: 400, eccErrors: 0, xidErrors: 0,
        throttling: false, timestamp: new Date().toISOString(),
      },
    }))
    setGpus(merged)
    setOverviewData(ov)
    setLoading(false)
  }, [observability.listGPUDevices, observability.getGPUMetrics, overview.get])

  useEffect(() => {
    void load()
    const iv = setInterval(() => void load(), 5000)
    return () => clearInterval(iv)
  }, [load])

  useEffect(() => {
    if (gpus.length === 0) return
    const avgUtil = Math.round(gpus.reduce((sum, g) => sum + g.metrics.utilization, 0) / gpus.length)
    const avgTemp = Math.round(gpus.reduce((sum, g) => sum + g.metrics.temperatureC, 0) / gpus.length)
    const critical = gpus.filter((g) => g.status === 'critical').length
    setHistory((prev) => {
      const next = [...prev, { time: new Date().toLocaleTimeString(), avgUtil, avgTemp, critical }]
      return next.slice(-30)
    })
  }, [gpus])

  if (loading) return <LoadingState />

  const healthDistribution = [
    { name: 'Healthy', value: gpus.filter((g) => g.status === 'healthy').length, color: 'green' },
    { name: 'Degraded', value: gpus.filter((g) => g.status === 'degraded').length, color: 'yellow' },
    { name: 'Critical', value: gpus.filter((g) => g.status === 'critical').length, color: 'red' },
    { name: 'Offline', value: gpus.filter((g) => g.status === 'offline').length, color: 'gray' },
  ].filter((d) => d.value > 0)

  const vendorDistribution = Object.entries(
    gpus.reduce<Record<string, number>>((acc, g) => { acc[g.vendor] = (acc[g.vendor] ?? 0) + 1; return acc }, {}),
  ).map(([name, value]) => ({ name, value, color: name === 'nvidia' ? 'green' : 'orange' }))

  return (
    <>
      <PageHeader
        title={t('dashboards.title')}
        subtitle="Live GPU fleet and mission operations views"
      />

      <Tabs defaultValue="gpu-fleet">
        <Tabs.List mb="xl">
          <Tabs.Tab value="gpu-fleet" leftSection={<IconCpu size={14} />}>{t('dashboards.gpuFleet')}</Tabs.Tab>
          <Tabs.Tab value="mission-ops" leftSection={<IconActivity size={14} />}>{t('dashboards.missionOps')}</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="gpu-fleet">
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="xl">
            <Card withBorder><Text size="xs" c="dimmed">Total GPUs</Text><Text size="xl" fw={700}>{gpus.length}</Text></Card>
            <Card withBorder>
              <Text size="xs" c="dimmed">Avg Utilization</Text>
              <Text size="xl" fw={700} c="blue">
                {gpus.length > 0 ? Math.round(gpus.reduce((s, g) => s + g.metrics.utilization, 0) / gpus.length) : 0}%
              </Text>
            </Card>
            <Card withBorder>
              <Text size="xs" c="dimmed">Avg Temperature</Text>
              <Text size="xl" fw={700} c="orange">
                {gpus.length > 0 ? Math.round(gpus.reduce((s, g) => s + g.metrics.temperatureC, 0) / gpus.length) : 0}°C
              </Text>
            </Card>
            <Card withBorder>
              <Text size="xs" c="dimmed">Critical GPUs</Text>
              <Text size="xl" fw={700} c="red">{gpus.filter((g) => g.status === 'critical').length}</Text>
            </Card>
          </SimpleGrid>

          <SimpleGrid cols={{ base: 1, lg: 3 }} mb="xl">
            <Card withBorder style={{ gridColumn: '1 / 3' }}>
              <Text fw={500} mb="md">Fleet Utilization & Temperature (live)</Text>
              {history.length > 1 ? (
                <AreaChart
                  h={250}
                  data={history}
                  dataKey="time"
                  series={[
                    { name: 'avgUtil', label: 'Avg Util %', color: 'blue' },
                    { name: 'avgTemp', label: 'Avg Temp °C', color: 'orange' },
                  ]}
                  curveType="monotone"
                  withDots={false}
                  withLegend
                />
              ) : (
                <Text size="sm" c="dimmed">Collecting data...</Text>
              )}
            </Card>
            <Stack gap="md">
              <Card withBorder>
                <Text fw={500} mb="md">Health Distribution</Text>
                {healthDistribution.length > 0 && (
                  <DonutChart data={healthDistribution} withLabelsLine withLabels size={150} mx="auto" />
                )}
              </Card>
              <Card withBorder>
                <Text fw={500} mb="sm">Vendor Split</Text>
                {vendorDistribution.length > 0 && (
                  <DonutChart data={vendorDistribution} size={100} mx="auto" withLabels />
                )}
              </Card>
            </Stack>
          </SimpleGrid>

          <Card withBorder>
            <Text fw={500} mb="md">Power Draw by Host</Text>
            <BarChart
              h={200}
              data={Object.entries(
                gpus.reduce<Record<string, number>>((acc, g) => {
                  acc[g.serverId] = (acc[g.serverId] ?? 0) + g.metrics.powerDrawW
                  return acc
                }, {}),
              ).map(([host, power]) => ({ host, power: Math.round(power) }))}
              dataKey="host"
              series={[{ name: 'power', label: 'Total Power (W)', color: 'violet' }]}
            />
          </Card>
        </Tabs.Panel>

        <Tabs.Panel value="mission-ops">
          {overviewData && (
            <>
              <SimpleGrid cols={{ base: 2, sm: 4 }} mb="xl">
                <Card withBorder><Text size="xs" c="dimmed">Active Missions</Text><Text size="xl" fw={700} c="blue">{overviewData.kpis.activeMissions}</Text></Card>
                <Card withBorder><Text size="xs" c="dimmed">Running Runs</Text><Text size="xl" fw={700} c="green">{overviewData.kpis.runningRuns}</Text></Card>
                <Card withBorder><Text size="xs" c="dimmed">Active Alerts</Text><Text size="xl" fw={700} c="red">{overviewData.kpis.activeAlerts}</Text></Card>
                <Card withBorder><Text size="xs" c="dimmed">Connected Planes</Text><Text size="xl" fw={700}>{overviewData.kpis.connectedPlanes}</Text></Card>
              </SimpleGrid>

              <Card withBorder mb="xl">
                <Group justify="space-between" mb="md">
                  <Text fw={500}>Recent Runs</Text>
                  <Badge>{overviewData.recentRuns.length}</Badge>
                </Group>
                {overviewData.recentRuns.length > 0 ? (
                  <BarChart
                    h={200}
                    data={overviewData.recentRuns.slice(0, 10).map((r) => ({
                      id: r.id.slice(0, 6),
                      status: r.status === 'succeeded' ? 1 : 0,
                    }))}
                    dataKey="id"
                    series={[{ name: 'status', label: 'Succeeded', color: 'green' }]}
                  />
                ) : (
                  <Text size="sm" c="dimmed">No runs yet.</Text>
                )}
              </Card>

              <Card withBorder>
                <Group mb="md" justify="space-between">
                  <Text fw={500}>{t('dashboards.createDisabled')}</Text>
                  <Button size="xs" variant="default" disabled leftSection={<IconChartArea size={12} />}>
                    New Dashboard
                  </Button>
                </Group>
                <Text size="sm" c="dimmed">
                  Custom dashboard builder is on the roadmap. Stay tuned for drag-and-drop widget configuration.
                </Text>
              </Card>
            </>
          )}
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
