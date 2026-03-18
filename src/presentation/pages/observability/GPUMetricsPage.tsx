import { useEffect, useState, useCallback, useRef } from 'react'
import {
  SimpleGrid, Card, Text, Group, Select, TextInput, Stack,
  Table, ActionIcon, Tooltip, Progress, ThemeIcon, Button, RingProgress, Center, SegmentedControl,
} from '@mantine/core'
import { AreaChart } from '@mantine/charts'
import { IconSearch, IconRefresh, IconAlertCircle, IconActivity, IconSparkles } from '@tabler/icons-react'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { GPUDevice, GPUMetrics } from '@/domain/gpu/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'

type GPUWithMetrics = GPUDevice & { metrics: GPUMetrics }
type TempUnit = 'C' | 'F'

function healthColor(health: string) {
  if (health === 'healthy') return 'green'
  if (health === 'degraded') return 'yellow'
  if (health === 'critical') return 'red'
  return 'gray'
}

function toDisplayTemp(celsius: number, unit: TempUnit): number {
  if (unit === 'F') return Math.round((celsius * 9 / 5 + 32) * 10) / 10
  return Math.round(celsius * 10) / 10
}

function tempUnitLabel(unit: TempUnit): string {
  return unit === 'C' ? '°C' : '°F'
}

function GaugeCard({ label, value, max, unit, color }: { label: string; value: number; max: number; unit: string; color: string }) {
  const pct = Math.round((value / max) * 100)
  return (
    <Card withBorder p="xs">
      <Text size="xs" c="dimmed" mb={4}>{label}</Text>
      <Progress value={pct} color={color} size="sm" mb={4} />
      <Text size="sm" fw={500}>{value}{unit} <Text span size="xs" c="dimmed">/ {max}{unit}</Text></Text>
    </Card>
  )
}

export function GPUMetricsPage() {
  const { observability, missions, platform } = useApp()
  const navigate = useNavigate()

  const [gpus, setGpus] = useState<GPUWithMetrics[]>([])
  const [selected, setSelected] = useState<GPUWithMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [healthFilter, setHealthFilter] = useState<string | null>(null)
  const [hostFilter, setHostFilter] = useState<string | null>(null)
  const [history, setHistory] = useState<{ time: string; util: number; temp: number; power: number }[]>([])
  const [refreshInterval, setRefreshInterval] = useState<number | null>(5000)
  const [tempUnit, setTempUnit] = useState<TempUnit>('C')
  const [defaultModelId, setDefaultModelId] = useState('model-gpt4o')

  // Use a ref to read `selected` inside load() without adding it as a dependency,
  // which would cause an infinite re-render loop (load → setSelected → new load → ...).
  const selectedRef = useRef<GPUWithMetrics | null>(null)
  selectedRef.current = selected

  const load = useCallback(async () => {
    try {
      const [devices, metricsList] = await Promise.all([
        observability.listGPUDevices.execute({}),
        observability.getGPUMetrics.execute(),
      ])
      const metricsById = new Map(metricsList.map((m) => [m.gpuId, m]))
      const merged: GPUWithMetrics[] = devices.map((gpu) => ({
        ...gpu,
        metrics: metricsById.get(gpu.id) ?? {
          gpuId: gpu.id,
          utilization: 0,
          memoryUsedMB: 0,
          memoryTotalMB: gpu.memoryGB * 1024,
          temperatureC: 0,
          powerDrawW: 0,
          powerLimitW: 400,
          eccErrors: 0,
          xidErrors: 0,
          throttling: false,
          timestamp: new Date().toISOString(),
        },
      }))
      setGpus(merged)
      if (!selectedRef.current && merged.length > 0) setSelected(merged[0])
      else if (selectedRef.current) {
        const updated = merged.find((g) => g.id === selectedRef.current!.id)
        if (updated) setSelected(updated)
      }
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [observability.listGPUDevices, observability.getGPUMetrics])

  useEffect(() => {
    platform.listModels.execute().then((models) => {
      const fallback = models.find((m) => m.isDefault) ?? models[0]
      if (fallback) setDefaultModelId(fallback.id)
    }).catch(() => null)
  }, [platform.listModels])

  useEffect(() => {
    void load()
    if (refreshInterval === null) return
    const iv = setInterval(() => void load(), refreshInterval)
    return () => clearInterval(iv)
  }, [load, refreshInterval])

  useEffect(() => {
    if (!selected) return
    setHistory((prev) => {
      const now = new Date().toLocaleTimeString()
      const next = [...prev, {
        time: now,
        util: Math.round(selected.metrics.utilization),
        temp: selected.metrics.temperatureC,
        power: Math.round(selected.metrics.powerDrawW),
      }]
      return next.slice(-30)
    })
  }, [selected])

  const handleCreateDiagMission = async () => {
    if (!selected) return
    const mission = await missions.create.execute({
      name: `GPU Diagnostics — ${selected.model}`,
      goal: `Run comprehensive diagnostics on GPU ${selected.model} (host: ${selected.hostId}). Check ECC errors, temperature, throttling, and XID errors.`,
      modelId: defaultModelId,
      target: selected.hostId,
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 120 },
      permissions: { allowedPlugins: ['nvidia-smi', 'ssh'], allowedTargets: [selected.hostId], guardrails: ['read-only'] },
      tags: ['gpu', 'diagnostics'],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'green' })
    navigate(`/missions/${mission.id}`)
  }

  const filtered = gpus.filter((g) => {
    if (healthFilter && g.health !== healthFilter) return false
    if (hostFilter && g.hostId !== hostFilter) return false
    if (search && !g.model.toLowerCase().includes(search.toLowerCase()) && !g.hostId.toLowerCase().includes(search.toLowerCase()) && !g.hostName.toLowerCase().includes(search.toLowerCase())) return false
    return true
  })

  const hostOptions = [...new Set(gpus.map((g) => g.hostId))].map((h) => ({ value: h, label: h }))

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title={t('gpu.titlePlural')}
        subtitle={`${gpus.length} devices · ${gpus.filter((g) => g.health === 'critical').length} critical`}
        actions={
          <Tooltip label="Refresh">
            <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
          </Tooltip>
        }
      />

      <Group gap="sm" mb="md" justify="space-between">
        <Group gap="sm">
        <TextInput
          placeholder={t('common.search')}
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          w={220}
        />
        <Select
          placeholder="Health"
          data={[
            { value: 'healthy', label: t('gpu.health.healthy') },
            { value: 'degraded', label: t('gpu.health.degraded') },
            { value: 'critical', label: t('gpu.health.critical') },
            { value: 'offline', label: t('gpu.health.offline') },
          ]}
          value={healthFilter}
          onChange={setHealthFilter}
          clearable
          w={140}
        />
        <Select
          placeholder="Host"
          data={hostOptions}
          value={hostFilter}
          onChange={setHostFilter}
          clearable
          w={160}
        />
        </Group>
        <Group gap="xs">
          <SegmentedControl
            value={tempUnit}
            onChange={(v) => setTempUnit(v as TempUnit)}
            data={[
              { value: 'C', label: '°C' },
              { value: 'F', label: '°F' },
            ]}
            size="xs"
          />
          <SegmentedControl
            value={refreshInterval === null ? 'manual' : String(refreshInterval)}
            onChange={(v) => setRefreshInterval(v === 'manual' ? null : Number(v))}
            data={[
              { value: 'manual', label: 'Manual' },
              { value: '1000', label: '1s' },
              { value: '5000', label: '5s' },
              { value: '10000', label: '10s' },
              { value: '30000', label: '30s' },
              { value: '60000', label: '1 min' },
              { value: '300000', label: '5 min' },
            ]}
            size="xs"
          />
        </Group>
      </Group>

      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="md">
        <Stack gap="md">
          {filtered.length === 0 ? (
            <EmptyState message={t('gpu.empty')} />
          ) : (
            <Card withBorder>
              <Text fw={500} mb="sm">{filtered.length} GPUs</Text>
              <Stack gap="sm" style={{ maxHeight: 600, overflow: 'auto' }}>
                {filtered.map((gpu) => (
                  <Card
                    key={gpu.id}
                    withBorder
                    p="lg"
                    style={{ cursor: 'pointer', minHeight: 170, borderColor: selected?.id === gpu.id ? 'var(--mantine-color-blue-5)' : undefined }}
                    onClick={() => setSelected(gpu)}
                  >
                    <Group justify="space-between" mb="md">
                      <Group gap="sm">
                        <ThemeIcon size="lg" color={healthColor(gpu.health)} variant="light">
                          <IconActivity size={16} />
                        </ThemeIcon>
                        <Stack gap={2}>
                          <Text size="md" fw={600}>{gpu.model}</Text>
                          <Text size="xs" c="dimmed">{gpu.hostName} · {gpu.vendor.toUpperCase()}</Text>
                        </Stack>
                      </Group>
                      <StatusBadge status={gpu.health} />
                    </Group>
                    <SimpleGrid cols={4} mt="sm" spacing="md">
                      <Stack gap={4}>
                        <Text size="xs" c="dimmed">{t('gpu.utilization')}</Text>
                        <Text size="sm" fw={600}>{Math.round(gpu.metrics.utilization)}%</Text>
                        <Progress value={Math.round(gpu.metrics.utilization)} size="sm" color="blue" />
                      </Stack>
                      <Stack gap={4}>
                        <Text size="xs" c="dimmed">{t('gpu.temperature')}</Text>
                        <Text size="sm" fw={600} c={gpu.metrics.temperatureC > 80 ? 'red' : undefined}>
                          {toDisplayTemp(gpu.metrics.temperatureC, tempUnit)}{tempUnitLabel(tempUnit)}
                        </Text>
                        <Progress value={(gpu.metrics.temperatureC / 100) * 100} size="sm" color={gpu.metrics.temperatureC > 80 ? 'red' : 'orange'} />
                      </Stack>
                      <Stack gap={4}>
                        <Text size="xs" c="dimmed">{t('gpu.powerDraw')}</Text>
                        <Text size="sm" fw={600}>{Math.round(gpu.metrics.powerDrawW)}W</Text>
                        <Progress value={(gpu.metrics.powerDrawW / gpu.metrics.powerLimitW) * 100} size="sm" color="violet" />
                      </Stack>
                      <Stack gap={4}>
                        <Text size="xs" c="dimmed">VRAM</Text>
                        <Text size="sm" fw={600}>{Math.round(gpu.metrics.memoryUsedMB / 1024)}G / {Math.round(gpu.metrics.memoryTotalMB / 1024)}G</Text>
                        <Progress value={(gpu.metrics.memoryUsedMB / gpu.metrics.memoryTotalMB) * 100} size="sm" color="cyan" />
                      </Stack>
                    </SimpleGrid>
                  </Card>
                ))}
              </Stack>
            </Card>
          )}
        </Stack>

        {selected && (
          <Stack gap="md">
            <Card withBorder>
              <Group justify="space-between" mb="md">
                <Text fw={500}>{selected.model}</Text>
                <Group gap="xs">
                  {(selected.metrics.eccErrors > 0 || selected.metrics.xidErrors > 0) && (
                    <Tooltip label={`${selected.metrics.eccErrors} ECC / ${selected.metrics.xidErrors} XID errors`}>
                      <ActionIcon color="red" variant="light" size="sm">
                        <IconAlertCircle size={14} />
                      </ActionIcon>
                    </Tooltip>
                  )}
                  <Button size="xs" variant="light" leftSection={<IconSparkles size={12} />}
                    onClick={() => void handleCreateDiagMission()}>
                    {t('gpu.createMissionDiagnostics')}
                  </Button>
                </Group>
              </Group>

              <SimpleGrid cols={2}>
                <Center>
                  <RingProgress
                    size={120}
                    sections={[{ value: Math.round(selected.metrics.utilization), color: 'blue' }]}
                    label={
                      <Center>
                        <Stack gap={0}>
                          <Text ta="center" fw={700} size="lg">{Math.round(selected.metrics.utilization)}%</Text>
                          <Text ta="center" size="xs" c="dimmed">Util</Text>
                        </Stack>
                      </Center>
                    }
                  />
                </Center>
                <Stack gap="xs">
                  <GaugeCard
                    label={t('gpu.temperature')}
                    value={toDisplayTemp(selected.metrics.temperatureC, tempUnit)}
                    max={toDisplayTemp(100, tempUnit)}
                    unit={tempUnitLabel(tempUnit)}
                    color={selected.metrics.temperatureC > 80 ? 'red' : 'orange'}
                  />
                  <GaugeCard label={t('gpu.powerDraw')} value={Math.round(selected.metrics.powerDrawW)} max={selected.metrics.powerLimitW} unit="W" color="violet" />
                  <GaugeCard label={t('gpu.memoryUsed')} value={Math.round(selected.metrics.memoryUsedMB / 1024)} max={Math.round(selected.metrics.memoryTotalMB / 1024)} unit="GB" color="cyan" />
                </Stack>
              </SimpleGrid>

              <Table mt="md" withRowBorders={false} fz="xs">
                <Table.Tbody>
                  <Table.Tr>
                    <Table.Td c="dimmed">{t('gpu.driverVersion')}</Table.Td>
                    <Table.Td>{selected.driverVersion}</Table.Td>
                    <Table.Td c="dimmed">{t('gpu.fanSpeed')}</Table.Td>
                    <Table.Td>{selected.metrics.fanSpeedPct != null ? `${selected.metrics.fanSpeedPct}%` : '—'}</Table.Td>
                  </Table.Tr>
                  <Table.Tr>
                    <Table.Td c="dimmed">{t('gpu.eccErrors')}</Table.Td>
                    <Table.Td c={selected.metrics.eccErrors > 0 ? 'red' : undefined}>{selected.metrics.eccErrors}</Table.Td>
                    <Table.Td c="dimmed">{t('gpu.xidErrors')}</Table.Td>
                    <Table.Td c={selected.metrics.xidErrors > 0 ? 'red' : undefined}>{selected.metrics.xidErrors}</Table.Td>
                  </Table.Tr>
                  <Table.Tr>
                    <Table.Td c="dimmed">{t('gpu.throttling')}</Table.Td>
                    <Table.Td c={selected.metrics.throttling ? 'orange' : 'green'}>{selected.metrics.throttling ? 'Yes' : 'No'}</Table.Td>
                    <Table.Td c="dimmed">Vendor</Table.Td>
                    <Table.Td>{selected.vendor.toUpperCase()}</Table.Td>
                  </Table.Tr>
                </Table.Tbody>
              </Table>
            </Card>

            {history.length > 2 && (
              <Card withBorder>
                <Text size="sm" fw={500} mb="sm">Live Metrics (last 30 samples)</Text>
                <AreaChart
                  h={200}
                  data={history.map((h) => ({
                    ...h,
                    temp: toDisplayTemp(h.temp, tempUnit),
                  }))}
                  dataKey="time"
                  series={[
                    { name: 'util', label: 'Utilization %', color: 'blue' },
                    { name: 'temp', label: `Temperature ${tempUnitLabel(tempUnit)}`, color: 'orange' },
                  ]}
                  curveType="monotone"
                  withDots={false}
                  withLegend
                />
              </Card>
            )}
          </Stack>
        )}
      </SimpleGrid>
    </>
  )
}
