import { useEffect, useState, useCallback } from 'react'
import { Grid, Card, Text, Group, ThemeIcon, SimpleGrid, Table, Badge, Stack, Timeline, ScrollArea, ActionIcon, Tooltip } from '@mantine/core'
import { useNavigate } from 'react-router-dom'
import {
  IconRocket, IconPlayerPlay, IconAlertTriangle, IconCpu,
  IconArrowUpRight, IconSitemap, IconCheck, IconX, IconClock, IconAlertCircle,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { OverviewData } from '@/application/dtos'
import { t } from '@/presentation/app/i18n'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { formatRelative } from '@/shared/utils/time'
import { useAuth } from '@/presentation/contexts/AuthContext'

function KPICard({
  label, value, sub, color, icon, onClick,
}: {
  label: string; value: number | string; sub?: string; color: string; icon: React.ReactNode; onClick?: () => void
}) {
  return (
    <Card
      withBorder
      radius="md"
      style={{ cursor: onClick ? 'pointer' : undefined }}
      onClick={onClick}
    >
      <Group justify="space-between" mb="xs">
        <Text size="sm" c="dimmed" fw={500}>{label}</Text>
        <ThemeIcon variant="light" color={color} size="md" radius="md">
          {icon}
        </ThemeIcon>
      </Group>
      <Text size="xl" fw={700}>{value}</Text>
      {sub && <Text size="xs" c="dimmed" mt={2}>{sub}</Text>}
    </Card>
  )
}

export function OverviewPage() {
  const { overview } = useApp()
  const { currentUser } = useAuth()
  const navigate = useNavigate()
  const [data, setData] = useState<OverviewData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const result = await overview.get.execute()
      setData(result)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [overview.get])

  useEffect(() => { void load() }, [load])

  if (loading) return <LoadingState rows={6} height={80} />
  if (error) return <ErrorState message={error} onRetry={load} />
  if (!data) return null

  const { kpis, recentRuns, topCriticalGPUs, activeAlerts, opsTimeline } = data

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

  const modulesByRole: Record<string, string[]> = {
    admin: ['Dashboard', 'Resources', 'Users', 'Settings'],
    owner: ['Dashboard', 'Resources', 'Team Views'],
    member: ['Dashboard', 'My Resources', 'Profile'],
  }

  const visibleModules = currentUser ? modulesByRole[currentUser.role] ?? ['Dashboard'] : ['Dashboard']

  return (
    <>
      <PageHeader title={t('overview.title')} subtitle="Real-time platform status" />

      {currentUser && (
        <Card withBorder radius="md" mb="md">
          <Group justify="space-between" align="flex-start">
            <div>
              <Text size="sm" c="dimmed">Current user</Text>
              <Text fw={600}>{currentUser.displayName} ({currentUser.username})</Text>
            </div>
            <Badge color={currentUser.role === 'admin' ? 'red' : currentUser.role === 'owner' ? 'blue' : 'gray'} variant="light">
              {currentUser.role}
            </Badge>
          </Group>
          <Group gap={8} mt="sm">
            {visibleModules.map((module) => (
              <Badge key={module} variant="outline">{module}</Badge>
            ))}
          </Group>
        </Card>
      )}

      <SimpleGrid cols={{ base: 2, sm: 4, lg: 7 }} mb="lg">
        <KPICard label={t('overview.kpi.activeMissions')} value={kpis.activeMissions} sub={`of ${kpis.totalMissions} total`} color="blue" icon={<IconRocket size={16} />} onClick={() => navigate('/missions?status=active')} />
        <KPICard label={t('overview.kpi.runningRuns')} value={kpis.runningRuns} sub={`of ${kpis.totalRuns} total`} color="violet" icon={<IconPlayerPlay size={16} />} onClick={() => navigate('/runs?status=running')} />
        <KPICard label={t('overview.kpi.activeAlerts')} value={kpis.activeAlerts} color={kpis.activeAlerts > 0 ? 'red' : 'green'} icon={<IconAlertTriangle size={16} />} onClick={() => navigate('/observability/alerts')} />
        <KPICard label={t('overview.kpi.criticalGPUs')} value={kpis.criticalGPUs} color={kpis.criticalGPUs > 0 ? 'red' : 'green'} icon={<IconCpu size={16} />} onClick={() => navigate('/observability/gpu-metrics?health=critical')} />
        <KPICard label={t('overview.kpi.connectedPlanes')} value={kpis.connectedPlanes} color="teal" icon={<IconSitemap size={16} />} onClick={() => navigate('/planes')} />
        <KPICard label={t('overview.kpi.totalMissions')} value={kpis.totalMissions} color="indigo" icon={<IconRocket size={16} />} onClick={() => navigate('/missions')} />
        <KPICard label={t('overview.kpi.totalRuns')} value={kpis.totalRuns} color="cyan" icon={<IconPlayerPlay size={16} />} onClick={() => navigate('/runs')} />
      </SimpleGrid>

      <Grid>
        <Grid.Col span={{ base: 12, lg: 8 }}>
          <Card withBorder radius="md" mb="md">
            <Group justify="space-between" mb="md">
              <Text fw={600}>{t('overview.recentRuns')}</Text>
              <Tooltip label={t('common.viewAll')}>
                <ActionIcon variant="subtle" onClick={() => navigate('/runs')}><IconArrowUpRight size={16} /></ActionIcon>
              </Tooltip>
            </Group>
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('mission.title')}</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                  <Table.Th>{t('run.trigger.manual')}</Table.Th>
                  <Table.Th>{t('run.queuedAt')}</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {recentRuns.slice(0, 8).map((run) => (
                  <Table.Tr key={run.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/runs/${run.id}`)}>
                    <Table.Td>
                      <Text size="sm" fw={500}>{run.missionName}</Text>
                      <Text size="xs" c="dimmed">{run.target}</Text>
                    </Table.Td>
                    <Table.Td><StatusBadge status={run.status} label={t(`run.status.${run.status}`)} /></Table.Td>
                    <Table.Td><Badge variant="outline" size="xs">{run.trigger}</Badge></Table.Td>
                    <Table.Td><Text size="xs" c="dimmed">{formatRelative(run.queuedAt)}</Text></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Card>

          <Card withBorder radius="md">
            <Group justify="space-between" mb="md">
              <Text fw={600}>{t('overview.gpuHealthSnapshot')}</Text>
              <Tooltip label={t('common.viewAll')}>
                <ActionIcon variant="subtle" onClick={() => navigate('/observability/gpu-metrics')}><IconArrowUpRight size={16} /></ActionIcon>
              </Tooltip>
            </Group>
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>GPU</Table.Th>
                  <Table.Th>{t('common.health')}</Table.Th>
                  <Table.Th>{t('gpu.temperature')}</Table.Th>
                  <Table.Th>{t('gpu.utilization')}</Table.Th>
                  <Table.Th>{t('gpu.eccErrors')}</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {topCriticalGPUs.map((gpu) => (
                  <Table.Tr key={gpu.id} style={{ cursor: 'pointer' }} onClick={() => navigate('/observability/gpu-metrics')}>
                    <Table.Td>
                      <Text size="sm" fw={500}>{gpu.id}</Text>
                      <Text size="xs" c="dimmed">{gpu.model} · {gpu.hostName}</Text>
                    </Table.Td>
                    <Table.Td><StatusBadge status={gpu.health} label={t(`gpu.health.${gpu.health}`)} /></Table.Td>
                    <Table.Td>
                      <Text size="sm" c={gpu.metrics.temperatureC > 88 ? 'red' : gpu.metrics.temperatureC > 80 ? 'yellow' : undefined}>
                        {gpu.metrics.temperatureC.toFixed(0)}°C
                      </Text>
                    </Table.Td>
                    <Table.Td><Text size="sm">{gpu.metrics.utilization.toFixed(0)}%</Text></Table.Td>
                    <Table.Td>
                      <Text size="sm" c={gpu.metrics.eccErrors > 0 ? 'red' : 'dimmed'}>
                        {gpu.metrics.eccErrors}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 4 }}>
          <Card withBorder radius="md" mb="md">
            <Group justify="space-between" mb="md">
              <Text fw={600}>{t('alert.titlePlural')}</Text>
              <Tooltip label={t('common.viewAll')}>
                <ActionIcon variant="subtle" onClick={() => navigate('/observability/alerts')}><IconArrowUpRight size={16} /></ActionIcon>
              </Tooltip>
            </Group>
            <Stack gap="xs">
              {activeAlerts.slice(0, 5).map((alert) => (
                <Card key={alert.id} withBorder radius="sm" p="xs" style={{ cursor: 'pointer' }} onClick={() => navigate('/observability/alerts')}>
                  <Group gap="xs" wrap="nowrap">
                    <ThemeIcon variant="light" color={alert.severity === 'critical' ? 'red' : 'yellow'} size="sm" radius="sm">
                      <IconAlertCircle size={12} />
                    </ThemeIcon>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <Text size="xs" fw={500} lineClamp={1}>{alert.title}</Text>
                      <Text size="xs" c="dimmed">{formatRelative(alert.createdAt)}</Text>
                    </div>
                    <Badge size="xs" color={alert.severity === 'critical' ? 'red' : 'yellow'}>{alert.severity}</Badge>
                  </Group>
                </Card>
              ))}
              {activeAlerts.length === 0 && (
                <Group gap="xs" py="xs">
                  <ThemeIcon color="green" variant="light" size="sm"><IconCheck size={12} /></ThemeIcon>
                  <Text size="sm" c="dimmed">{t('alert.empty')}</Text>
                </Group>
              )}
            </Stack>
          </Card>

          <Card withBorder radius="md">
            <Text fw={600} mb="md">{t('overview.opsTimeline')}</Text>
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
              </Timeline>
            </ScrollArea>
          </Card>
        </Grid.Col>
      </Grid>
    </>
  )
}
