import { useEffect, useState, useCallback } from 'react'
import {
  Card, Group, Text, Badge, Stack, Button, ThemeIcon, SimpleGrid,
  ActionIcon, Tabs, Anchor,
} from '@mantine/core'
import { useNavigate } from 'react-router-dom'
import { IconPlus, IconServer2, IconRefresh } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Plane, K8sCluster, SlurmCluster } from '@/domain/plane/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatRelative } from '@/shared/utils/time'

function planeIcon(_type: string) {
  return <IconServer2 size={20} />
}

function planeColor(status: string) {
  if (status === 'connected') return 'green'
  if (status === 'degraded') return 'yellow'
  if (status === 'disconnected') return 'red'
  return 'gray'
}

export function ManagementPlanesPage() {
  const { planes } = useApp()
  const navigate = useNavigate()

  const [items, setItems] = useState<Plane[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const result = await planes.list.execute()
      setItems(result)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [planes.list])

  useEffect(() => { void load() }, [load])

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  const k8s = items.filter((p) => p.type === 'kubernetes')
  const slurm = items.filter((p) => p.type === 'slurm')
  const connected = items.filter((p) => p.status === 'connected').length

  return (
    <>
      <PageHeader
        title={t('plane.titlePlural')}
        subtitle={`${items.length} planes · ${connected} connected`}
        actions={
          <Group gap="xs">
            <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
            <Button size="sm" leftSection={<IconPlus size={14} />} onClick={() => navigate('/planes/new')}>
              {t('plane.actions.register')}
            </Button>
          </Group>
        }
      />

      {items.length === 0 ? (
        <EmptyState
          message={t('plane.empty')}
          action={{ label: t('plane.actions.register'), onClick: () => navigate('/planes/new') }}
        />
      ) : (
        <>
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="xl">
            <Card withBorder><Text size="xs" c="dimmed">Total Planes</Text><Text size="xl" fw={700}>{items.length}</Text></Card>
            <Card withBorder><Text size="xs" c="dimmed">Connected</Text><Text size="xl" fw={700} c="green">{connected}</Text></Card>
            <Card withBorder><Text size="xs" c="dimmed">Kubernetes</Text><Text size="xl" fw={700} c="blue">{k8s.length}</Text></Card>
            <Card withBorder><Text size="xs" c="dimmed">Slurm</Text><Text size="xl" fw={700} c="violet">{slurm.length}</Text></Card>
          </SimpleGrid>

          <Tabs defaultValue="all">
            <Tabs.List mb="md">
              <Tabs.Tab value="all">All ({items.length})</Tabs.Tab>
              <Tabs.Tab value="kubernetes" leftSection={<IconServer2 size={14} />}>Kubernetes ({k8s.length})</Tabs.Tab>
              <Tabs.Tab value="slurm" leftSection={<IconServer2 size={14} />}>Slurm ({slurm.length})</Tabs.Tab>
            </Tabs.List>

            {(['all', 'kubernetes', 'slurm'] as const).map((tab) => (
              <Tabs.Panel key={tab} value={tab}>
                <Stack gap="md">
                  {(tab === 'all' ? items : items.filter((p) => p.type === tab)).map((plane) => (
                    <Card key={plane.id} withBorder>
                      <Group justify="space-between" mb="sm">
                        <Group gap="sm">
                          <ThemeIcon variant="light" color={planeColor(plane.status)} size="lg">
                            {planeIcon(plane.type)}
                          </ThemeIcon>
                          <Stack gap={0}>
                            <Group gap="xs">
                              <Anchor fw={500} onClick={() => navigate(plane.type === 'kubernetes' ? '/planes/kubernetes' : '/planes/slurm')}>
                                {plane.name}
                              </Anchor>
                              <StatusBadge status={plane.status} />
                              <Badge size="xs" variant="outline">{plane.type}</Badge>
                            </Group>
                            <Text size="xs" c="dimmed">{plane.endpointRef}</Text>
                          </Stack>
                        </Group>
                        <Text size="xs" c="dimmed">Last sync: {plane.lastSyncAt ? formatRelative(plane.lastSyncAt) : '—'}</Text>
                      </Group>

                      {plane.type === 'kubernetes' && (
                        <SimpleGrid cols={4}>
                          <Stack gap={0}><Text size="xs" c="dimmed">Nodes</Text><Text size="sm" fw={500}>{(plane as K8sCluster).nodeCount}</Text></Stack>
                          <Stack gap={0}><Text size="xs" c="dimmed">GPU Nodes</Text><Text size="sm" fw={500}>{(plane as K8sCluster).gpuNodeCount}</Text></Stack>
                          <Stack gap={0}><Text size="xs" c="dimmed">Version</Text><Text size="sm" fw={500}>{plane.version ?? '—'}</Text></Stack>
                          <Stack gap={0}><Text size="xs" c="dimmed">Addons</Text><Text size="sm" fw={500}>{(plane as K8sCluster).addons?.length ?? 0}</Text></Stack>
                        </SimpleGrid>
                      )}

                      {plane.type === 'slurm' && (
                        <SimpleGrid cols={4}>
                          <Stack gap={0}><Text size="xs" c="dimmed">Total Nodes</Text><Text size="sm" fw={500}>{(plane as SlurmCluster).totalNodes}</Text></Stack>
                          <Stack gap={0}><Text size="xs" c="dimmed">Partitions</Text><Text size="sm" fw={500}>{(plane as SlurmCluster).partitions?.length ?? 0}</Text></Stack>
                          <Stack gap={0}><Text size="xs" c="dimmed">Running Jobs</Text><Text size="sm" fw={500}>{(plane as SlurmCluster).recentJobs?.filter((j) => j.status === 'running').length ?? 0}</Text></Stack>
                          <Stack gap={0}><Text size="xs" c="dimmed">Version</Text><Text size="sm" fw={500}>{plane.version ?? '—'}</Text></Stack>
                        </SimpleGrid>
                      )}

                      {plane.labels && plane.labels.length > 0 && (
                        <Group gap={4} mt="sm">
                          {plane.labels.map((l) => <Badge key={l} size="xs" variant="dot">{l}</Badge>)}
                        </Group>
                      )}
                    </Card>
                  ))}
                </Stack>
              </Tabs.Panel>
            ))}
          </Tabs>
        </>
      )}
    </>
  )
}
