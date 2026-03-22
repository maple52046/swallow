import { useEffect, useState } from 'react'
import {
  Card, Group, Text, Badge, Stack, SimpleGrid, Button, ThemeIcon, Table,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { IconServer2, IconSparkles } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { SlurmCluster } from '@/domain/plane/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'

export function SlurmPage() {
  const { planes, missions, platform } = useApp()
  const navigate = useNavigate()

  const [clusters, setClusters] = useState<SlurmCluster[]>([])
  const [loading, setLoading] = useState(true)
  const [defaultModelId, setDefaultModelId] = useState('model-gpt4o')

  useEffect(() => {
    platform.listModels.execute().then((models) => {
      const fallback = models.find((m) => m.isDefault) ?? models[0]
      if (fallback) setDefaultModelId(fallback.id)
    }).catch(() => null)
  }, [platform.listModels])

  useEffect(() => {
    planes.list.execute().then((result) => {
      setClusters(result.filter((p): p is SlurmCluster => p.type === 'slurm'))
    }).catch(() => null).finally(() => setLoading(false))
  }, [planes.list])

  const handleMission = async (cluster: SlurmCluster, action: string) => {
    const goalMap: Record<string, string> = {
      health: `Check health of all nodes in Slurm cluster ${cluster.name}. Run sinfo, check drain/down states.`,
      logs: `Collect system logs from all Slurm nodes in cluster ${cluster.name}.`,
      resume: `Resume all drained/down nodes in Slurm cluster ${cluster.name}.`,
    }
    const mission = await missions.create.execute({
      name: `Slurm ${action} — ${cluster.name}`,
      goal: goalMap[action] ?? `Execute ${action} on Slurm cluster ${cluster.name}`,
      modelId: defaultModelId,
      target: cluster.id,
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 120 },
      permissions: { allowedPlugins: ['scontrol', 'ssh'], allowedTargets: [cluster.id], guardrails: ['read-only'] },
      tags: ['slurm', action],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'violet' })
    navigate(`/deprecated/missions/${mission.id}`)
  }

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader
        title={t('plane.type.slurm')}
        subtitle={`${clusters.length} cluster(s)`}
      />

      {clusters.length === 0 ? (
        <EmptyState message={t('plane.empty')} />
      ) : (
        <Stack gap="md">
          {clusters.map((cluster) => {
            const runningJobs = cluster.recentJobs?.filter((j) => j.status === 'running').length ?? 0
            const pendingJobs = cluster.recentJobs?.filter((j) => j.status === 'pending').length ?? 0

            return (
              <Card key={cluster.id} withBorder>
                <Group justify="space-between" mb="md">
                  <Group gap="sm">
                    <ThemeIcon variant="light" color="violet" size="lg"><IconServer2 size={20} /></ThemeIcon>
                    <Stack gap={0}>
                      <Group gap="xs">
                        <Text fw={500}>{cluster.name}</Text>
                        <StatusBadge status={cluster.status} />
                      </Group>
                      <Text size="xs" c="dimmed">{cluster.endpointRef}</Text>
                    </Stack>
                  </Group>
                  <Group gap="xs">
                    <Button size="xs" variant="light" onClick={() => void handleMission(cluster, 'health')}>{t('plane.actions.resume')}</Button>
                    <Button size="xs" variant="light" leftSection={<IconSparkles size={12} />} onClick={() => void handleMission(cluster, 'logs')}>{t('plane.actions.collectLogs')}</Button>
                  </Group>
                </Group>

                <SimpleGrid cols={4} mb="md">
                  <Stack gap={0}><Text size="xs" c="dimmed">Total Nodes</Text><Text size="sm" fw={500}>{cluster.totalNodes}</Text></Stack>
                  <Stack gap={0}><Text size="xs" c="dimmed">Alloc Nodes</Text><Text size="sm" fw={500} c="blue">{cluster.allocNodes}</Text></Stack>
                  <Stack gap={0}><Text size="xs" c="dimmed">Running Jobs</Text><Text size="sm" fw={500} c="blue">{runningJobs}</Text></Stack>
                  <Stack gap={0}><Text size="xs" c="dimmed">Pending Jobs</Text><Text size="sm" fw={500} c="yellow">{pendingJobs}</Text></Stack>
                </SimpleGrid>

                {cluster.partitions && cluster.partitions.length > 0 && (
                  <>
                    <Text size="xs" fw={500} mb="xs">{t('plane.partitions')}</Text>
                    <Table fz="xs">
                      <Table.Thead>
                        <Table.Tr>
                          <Table.Th>Name</Table.Th>
                          <Table.Th>Nodes</Table.Th>
                          <Table.Th>Alloc</Table.Th>
                          <Table.Th>Idle</Table.Th>
                          <Table.Th>GPUs</Table.Th>
                          <Table.Th>{t('common.status')}</Table.Th>
                        </Table.Tr>
                      </Table.Thead>
                      <Table.Tbody>
                        {cluster.partitions.map((part) => (
                          <Table.Tr key={part.name}>
                            <Table.Td fw={500}>{part.name}</Table.Td>
                            <Table.Td>{part.nodeCount}</Table.Td>
                            <Table.Td>{part.allocNodes}</Table.Td>
                            <Table.Td>{part.idleNodes}</Table.Td>
                            <Table.Td>{part.totalGPUs}</Table.Td>
                            <Table.Td><Badge size="xs" color={part.state === 'up' ? 'green' : 'red'}>{part.state}</Badge></Table.Td>
                          </Table.Tr>
                        ))}
                      </Table.Tbody>
                    </Table>
                  </>
                )}
              </Card>
            )
          })}
        </Stack>
      )}
    </>
  )
}
