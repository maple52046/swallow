import { useEffect, useState } from 'react'
import {
  Card, Group, Text, Badge, Stack, SimpleGrid, Button, ThemeIcon, Table,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { IconServer2, IconSparkles } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { K8sCluster } from '@/domain/plane/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'

export function KubernetesPage() {
  const { planes, missions, platform } = useApp()
  const navigate = useNavigate()

  const [clusters, setClusters] = useState<K8sCluster[]>([])
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
      setClusters(result.filter((p): p is K8sCluster => p.type === 'kubernetes'))
    }).catch(() => null).finally(() => setLoading(false))
  }, [planes.list])

  const handleMission = async (cluster: K8sCluster, action: string) => {
    const goalMap: Record<string, string> = {
      drain: `Drain all GPU nodes in Kubernetes cluster ${cluster.name}. Cordon nodes, wait for pods to terminate.`,
      cordon: `Cordon all GPU nodes in Kubernetes cluster ${cluster.name} to prevent new pod scheduling.`,
      logs: `Collect logs from all pods in GPU namespaces in cluster ${cluster.name}.`,
      upgrade: `Upgrade addons in cluster ${cluster.name} to latest stable versions.`,
    }
    const mission = await missions.create.execute({
      name: `K8s ${action} — ${cluster.name}`,
      goal: goalMap[action] ?? `Execute ${action} on cluster ${cluster.name}`,
      modelId: defaultModelId,
      target: cluster.id,
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 120 },
      permissions: { allowedPlugins: ['kubectl', 'ssh'], allowedTargets: [cluster.id], guardrails: ['require-confirmation'] },
      tags: ['kubernetes', action],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'blue' })
    navigate(`/missions/${mission.id}`)
  }

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader
        title={t('plane.type.kubernetes')}
        subtitle={`${clusters.length} cluster(s)`}
      />

      {clusters.length === 0 ? (
        <EmptyState message={t('plane.empty')} />
      ) : (
        <Stack gap="md">
          {clusters.map((cluster) => (
            <Card key={cluster.id} withBorder>
              <Group justify="space-between" mb="md">
                <Group gap="sm">
                  <ThemeIcon variant="light" color="blue" size="lg"><IconServer2 size={20} /></ThemeIcon>
                  <Stack gap={0}>
                    <Group gap="xs">
                      <Text fw={500}>{cluster.name}</Text>
                      <StatusBadge status={cluster.status} />
                    </Group>
                    <Text size="xs" c="dimmed">{cluster.endpointRef}</Text>
                  </Stack>
                </Group>
                <Group gap="xs">
                  <Button size="xs" variant="light" onClick={() => void handleMission(cluster, 'drain')}>{t('plane.actions.drain')}</Button>
                  <Button size="xs" variant="light" onClick={() => void handleMission(cluster, 'cordon')}>{t('plane.actions.cordon')}</Button>
                  <Button size="xs" variant="light" leftSection={<IconSparkles size={12} />} onClick={() => void handleMission(cluster, 'logs')}>{t('plane.actions.collectLogs')}</Button>
                </Group>
              </Group>

              <SimpleGrid cols={4} mb="md">
                <Stack gap={0}><Text size="xs" c="dimmed">{t('plane.nodes')}</Text><Text size="sm" fw={500}>{cluster.nodeCount}</Text></Stack>
                <Stack gap={0}><Text size="xs" c="dimmed">{t('plane.gpuNodes')}</Text><Text size="sm" fw={500}>{cluster.gpuNodeCount}</Text></Stack>
                <Stack gap={0}><Text size="xs" c="dimmed">Version</Text><Text size="sm" fw={500}>{cluster.version ?? '—'}</Text></Stack>
                <Stack gap={0}><Text size="xs" c="dimmed">{t('plane.addons')}</Text><Text size="sm" fw={500}>{cluster.addons?.length ?? 0}</Text></Stack>
              </SimpleGrid>

              {cluster.nodes && cluster.nodes.length > 0 && (
                <>
                  <Text size="xs" fw={500} mb="xs">Nodes</Text>
                  <Table fz="xs">
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>Name</Table.Th>
                        <Table.Th>Role</Table.Th>
                        <Table.Th>GPUs</Table.Th>
                        <Table.Th>CPUs</Table.Th>
                        <Table.Th>{t('common.status')}</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {cluster.nodes.map((node) => (
                        <Table.Tr key={node.name}>
                          <Table.Td ff="mono">{node.name}</Table.Td>
                          <Table.Td><Badge size="xs" variant="outline">{node.role}</Badge></Table.Td>
                          <Table.Td>{node.gpuCount}</Table.Td>
                          <Table.Td>{node.cpuCount}</Table.Td>
                          <Table.Td><Badge size="xs" color={node.status === 'ready' ? 'green' : 'red'}>{node.status}</Badge></Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                </>
              )}

              {cluster.addons && cluster.addons.length > 0 && (
                <>
                  <Text size="xs" fw={500} mb="xs" mt="md">{t('plane.addons')}</Text>
                  <Table fz="xs">
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>Name</Table.Th>
                        <Table.Th>{t('common.version')}</Table.Th>
                        <Table.Th>{t('common.status')}</Table.Th>
                        <Table.Th></Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {cluster.addons.map((addon) => (
                        <Table.Tr key={addon.name}>
                          <Table.Td>{addon.name}</Table.Td>
                          <Table.Td><Badge size="xs" variant="outline">{addon.version}</Badge></Table.Td>
                          <Table.Td><Badge size="xs" color={addon.status === 'healthy' ? 'green' : 'red'}>{addon.status}</Badge></Table.Td>
                          <Table.Td>
                            {addon.latestVersion && addon.latestVersion !== addon.version && (
                              <Button size="xs" variant="subtle" onClick={() => void handleMission(cluster, 'upgrade')}>{t('plane.actions.upgrade')}</Button>
                            )}
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                </>
              )}
            </Card>
          ))}
        </Stack>
      )}
    </>
  )
}
