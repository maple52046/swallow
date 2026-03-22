import { useEffect, useState, useCallback } from 'react'
import {
  Card, Group, Text, Stack, SimpleGrid, ThemeIcon, Button,
  Progress, ActionIcon, Collapse,
} from '@mantine/core'
import { BarChart } from '@mantine/charts'
import { IconChartBar, IconEye, IconSparkles, IconRefresh } from '@tabler/icons-react'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { GPUProfile } from '@/domain/gpu/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { formatRelative } from '@/shared/utils/time'

export function GPUProfilingPage() {
  const { observability, missions, platform } = useApp()
  const navigate = useNavigate()

  const [profiles, setProfiles] = useState<GPUProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [defaultModelId, setDefaultModelId] = useState('model-gpt4o')

  useEffect(() => {
    platform.listModels.execute().then((models) => {
      const fallback = models.find((m) => m.isDefault) ?? models[0]
      if (fallback) setDefaultModelId(fallback.id)
    }).catch(() => null)
  }, [platform.listModels])

  const load = useCallback(async () => {
    try {
      const result = await observability.listGPUProfiles.execute()
      setProfiles(result)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [observability.listGPUProfiles])

  useEffect(() => { void load() }, [load])

  const handleCreateProfilingMission = async (profile: GPUProfile) => {
    const mission = await missions.create.execute({
      name: `GPU Profiling Analysis — ${profile.gpuId}`,
      goal: `Analyze profiling data for GPU ${profile.gpuModel} on ${profile.hostName}. Identify performance bottlenecks and optimization opportunities.`,
      modelId: defaultModelId,
      target: profile.hostId,
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 300 },
      permissions: { allowedPlugins: ['gpu-profiler', 'nvidia-smi', 'ssh'], allowedTargets: [profile.hostId], guardrails: ['read-only'] },
      tags: ['gpu', 'profiling'],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'green' })
    navigate(`/deprecated/missions/${mission.id}`)
  }

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title={t('profiling.title')}
        subtitle={`${profiles.length} profiling ${profiles.length === 1 ? 'profile' : 'profiles'}`}
        actions={
          <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
        }
      />

      {profiles.length === 0 ? (
        <EmptyState message={t('profiling.empty')} icon={<IconChartBar size={32} />} />
      ) : (
        <Stack gap="md">
          {profiles.map((profile) => (
            <Card key={profile.id} withBorder>
              <Group justify="space-between" mb="sm">
                <Group gap="sm">
                  <ThemeIcon variant="light" color="violet"><IconChartBar size={16} /></ThemeIcon>
                  <Stack gap={0}>
                    <Text fw={500}>{profile.gpuModel}</Text>
                    <Text size="xs" c="dimmed">{profile.hostName} · {formatRelative(profile.startedAt)}</Text>
                  </Stack>
                </Group>
                <Group gap="xs">
                  <Button size="xs" variant="light" leftSection={<IconSparkles size={12} />}
                    onClick={() => void handleCreateProfilingMission(profile)}>
                    Analyze
                  </Button>
                  <ActionIcon variant="subtle" onClick={() => setExpandedId(expandedId === profile.id ? null : profile.id)}>
                    <IconEye size={16} />
                  </ActionIcon>
                </Group>
              </Group>

              <SimpleGrid cols={4} mb="sm">
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">Compute Util</Text>
                  <Progress value={profile.summary.computeUtilizationPct} size="xs" color="blue" />
                  <Text size="xs">{profile.summary.computeUtilizationPct}%</Text>
                </Stack>
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">Memory Util</Text>
                  <Progress value={profile.summary.memoryUtilizationPct} size="xs" color="cyan" />
                  <Text size="xs">{profile.summary.memoryUtilizationPct}%</Text>
                </Stack>
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">Roofline Eff.</Text>
                  <Progress value={profile.summary.rooflineEfficiency * 100} size="xs" color="green" />
                  <Text size="xs">{Math.round(profile.summary.rooflineEfficiency * 100)}%</Text>
                </Stack>
                <Stack gap={2}>
                  <Text size="xs" c="dimmed">Mem BW (GB/s)</Text>
                  <Text size="xs" fw={500}>{profile.summary.memoryBandwidthGBs.toFixed(1)}</Text>
                </Stack>
              </SimpleGrid>

              <Collapse in={expandedId === profile.id}>
                <Stack gap="md" mt="md">
                  <Text size="sm" fw={500}>{t('profiling.report.kernels')}</Text>
                  {profile.summary.topKernels && profile.summary.topKernels.length > 0 && (
                    <>
                      <BarChart
                        h={180}
                        data={profile.summary.topKernels.slice(0, 8).map((k) => ({
                          name: k.name.length > 24 ? k.name.slice(0, 24) + '…' : k.name,
                          pct: k.durationPct,
                        }))}
                        dataKey="name"
                        series={[{ name: 'pct', label: '% of total', color: 'violet' }]}
                      />
                      <Stack gap={4}>
                        {profile.summary.topKernels.map((k, i) => (
                          <Group key={i} justify="space-between">
                            <Text size="xs" ff="mono" style={{ flex: 1 }} lineClamp={1}>{k.name}</Text>
                            <Group gap="md">
                              <Text size="xs" c="dimmed">{k.callCount} calls</Text>
                              <Text size="xs" fw={500}>{k.durationPct}%</Text>
                            </Group>
                          </Group>
                        ))}
                      </Stack>
                    </>
                  )}
                </Stack>
              </Collapse>
            </Card>
          ))}
        </Stack>
      )}
    </>
  )
}
