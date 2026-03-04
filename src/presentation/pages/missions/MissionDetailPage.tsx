import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { notifications } from '@mantine/notifications'
import {
  Card, Group, Button, Text, Stack, Badge, Table, Title, SimpleGrid,
  List, ThemeIcon, Divider, ActionIcon, Menu,
} from '@mantine/core'
import {
  IconArrowLeft, IconPlayerPlay, IconPlayerPause, IconArchive, IconCopy, IconDots,
  IconCheck, IconClock, IconEdit,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Mission } from '@/domain/mission/types'
import type { Run } from '@/domain/run/types'
import { t } from '@/presentation/app/i18n'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { formatRelative, formatDuration } from '@/shared/utils/time'

export function MissionDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { missions, runs } = useApp()

  const [mission, setMission] = useState<Mission | null>(null)
  const [missionRuns, setMissionRuns] = useState<Run[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!id) return
    try {
      setLoading(true)
      setError(null)
      const [m, r] = await Promise.all([
        missions.get.execute(id),
        runs.list.execute({ missionId: id }),
      ])
      if (!m) { setError('Mission not found'); return }
      setMission(m)
      setMissionRuns(r)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [id, missions.get, runs.list])

  useEffect(() => { void load() }, [load])

  const handleRunNow = async () => {
    if (!mission) return
    try {
      const run = await missions.runNow.execute(mission.id)
      notifications.show({ title: 'Run started', message: `Run ${run.id.slice(0, 8)}...`, color: 'green' })
      navigate(`/runs/${run.id}`)
    } catch (e) { notifications.show({ title: 'Error', message: String(e), color: 'red' }) }
  }

  const handlePause = async () => {
    if (!mission) return
    try { await missions.pause.execute(mission.id); notifications.show({ title: 'Paused', message: '', color: 'yellow' }); void load() }
    catch (e) { notifications.show({ title: 'Error', message: String(e), color: 'red' }) }
  }

  const handleResume = async () => {
    if (!mission) return
    try { await missions.resume.execute(mission.id); notifications.show({ title: 'Resumed', message: '', color: 'green' }); void load() }
    catch (e) { notifications.show({ title: 'Error', message: String(e), color: 'red' }) }
  }

  const handleClone = async () => {
    if (!mission) return
    try {
      const cloned = await missions.clone.execute(mission.id)
      notifications.show({ title: 'Cloned', message: `Created "${cloned.name}"`, color: 'blue' })
      navigate(`/missions/${cloned.id}`)
    } catch (e) { notifications.show({ title: 'Error', message: String(e), color: 'red' }) }
  }

  if (loading) return <LoadingState rows={8} height={60} />
  if (error) return <ErrorState message={error} onRetry={load} />
  if (!mission) return null

  return (
    <>
      <Group mb="md">
        <ActionIcon variant="subtle" onClick={() => navigate('/missions')}><IconArrowLeft size={16} /></ActionIcon>
        <Text size="sm" c="dimmed">{t('mission.titlePlural')}</Text>
      </Group>

      <PageHeader
        title={mission.name}
        subtitle={mission.goal}
        actions={
          <Group gap="xs">
            {mission.status !== 'archived' && mission.status !== 'paused' && (
              <Button leftSection={<IconPlayerPlay size={14} />} size="sm" onClick={() => void handleRunNow()}>
                {t('mission.actions.runNow')}
              </Button>
            )}
            <Menu position="bottom-end" withArrow>
              <Menu.Target>
                <Button variant="default" size="sm" rightSection={<IconDots size={14} />}>{t('common.actions')}</Button>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Item leftSection={<IconEdit size={14} />} onClick={() => navigate(`/missions/new?clone=${mission.id}`)}>
                  {t('mission.actions.editPlan')}
                </Menu.Item>
                {mission.status === 'active' && (
                  <Menu.Item leftSection={<IconPlayerPause size={14} />} onClick={() => void handlePause()}>{t('mission.actions.pause')}</Menu.Item>
                )}
                {mission.status === 'paused' && (
                  <Menu.Item leftSection={<IconPlayerPlay size={14} />} onClick={() => void handleResume()}>{t('mission.actions.resume')}</Menu.Item>
                )}
                <Menu.Item leftSection={<IconCopy size={14} />} onClick={() => void handleClone()}>{t('mission.actions.clone')}</Menu.Item>
                {mission.status !== 'archived' && (
                  <>
                    <Menu.Divider />
                    <Menu.Item leftSection={<IconArchive size={14} />} color="red" onClick={async () => { await missions.archive.execute(mission.id); navigate('/missions') }}>
                      {t('mission.actions.archive')}
                    </Menu.Item>
                  </>
                )}
              </Menu.Dropdown>
            </Menu>
          </Group>
        }
      />

      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="lg">
        <Card withBorder radius="md" p="sm">
          <Text size="xs" c="dimmed">{t('common.status')}</Text>
          <StatusBadge status={mission.status} label={t(`mission.status.${mission.status}`)} mt={4} />
        </Card>
        <Card withBorder radius="md" p="sm">
          <Text size="xs" c="dimmed">{t('mission.trigger.manual')}</Text>
          <Text size="sm" fw={500} mt={4}>{mission.trigger}{mission.schedule ? ` · ${mission.schedule}` : ''}</Text>
        </Card>
        <Card withBorder radius="md" p="sm">
          <Text size="xs" c="dimmed">{t('mission.model')}</Text>
          <Text size="sm" fw={500} mt={4}>{mission.model}</Text>
        </Card>
        <Card withBorder radius="md" p="sm">
          <Text size="xs" c="dimmed">{t('mission.runCount')}</Text>
          <Text size="sm" fw={500} mt={4}>{mission.runCount} · {t('mission.lastRunAt')}: {formatRelative(mission.lastRunAt)}</Text>
        </Card>
      </SimpleGrid>

      <Stack gap="md">
        <Card withBorder radius="md">
          <Title order={5} mb="md">{t('mission.plan')} — {mission.plan.steps.length} steps</Title>
          <Table>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>#</Table.Th>
                <Table.Th>{t('common.name')}</Table.Th>
                <Table.Th>Plugin</Table.Th>
                <Table.Th>Action</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {mission.plan.steps.map((step) => (
                <Table.Tr key={step.id}>
                  <Table.Td>
                    <ThemeIcon variant="light" size="sm" radius="xl">{step.order}</ThemeIcon>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" fw={500}>{step.name}</Text>
                    <Text size="xs" c="dimmed">{step.description}</Text>
                  </Table.Td>
                  <Table.Td><Badge variant="outline" size="xs">{step.plugin}</Badge></Table.Td>
                  <Table.Td><Text size="xs">{step.action}</Text></Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Card>

        <Card withBorder radius="md">
          <Title order={5} mb="md">{t('mission.permissions')}</Title>
          <SimpleGrid cols={3}>
            <div>
              <Text size="xs" c="dimmed" mb={4}>{t('mission.allowedPlugins')}</Text>
              <Stack gap={2}>
                {mission.permissions.allowedPlugins.map((p) => (
                  <Badge key={p} variant="light" size="sm">{p}</Badge>
                ))}
              </Stack>
            </div>
            <div>
              <Text size="xs" c="dimmed" mb={4}>{t('mission.allowedTargets')}</Text>
              <Stack gap={2}>
                {mission.permissions.allowedTargets.map((t_) => (
                  <Badge key={t_} variant="light" color="teal" size="sm">{t_}</Badge>
                ))}
              </Stack>
            </div>
            <div>
              <Text size="xs" c="dimmed" mb={4}>{t('mission.guardrails')}</Text>
              <List spacing={2} size="xs">
                {mission.permissions.guardrails.length === 0
                  ? <Text size="xs" c="dimmed">None</Text>
                  : mission.permissions.guardrails.map((g) => (
                    <List.Item key={g} icon={<ThemeIcon color="orange" variant="light" size="xs" radius="xl"><IconCheck size={10} /></ThemeIcon>}>
                      {g}
                    </List.Item>
                  ))}
              </List>
            </div>
          </SimpleGrid>
        </Card>

        <Card withBorder radius="md">
          <Group justify="space-between" mb="md">
            <Title order={5}>{t('run.titlePlural')}</Title>
            <Button variant="subtle" size="xs" onClick={() => navigate(`/runs?missionId=${mission.id}`)}>
              {t('common.viewAll')}
            </Button>
          </Group>
          {missionRuns.length === 0 ? (
            <Text size="sm" c="dimmed">{t('run.empty')}</Text>
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Run ID</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                  <Table.Th>{t('run.trigger.manual')}</Table.Th>
                  <Table.Th>{t('run.duration')}</Table.Th>
                  <Table.Th>{t('run.queuedAt')}</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {missionRuns.slice(0, 10).map((run) => (
                  <Table.Tr key={run.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/runs/${run.id}`)}>
                    <Table.Td><Text size="xs" ff="monospace">{run.id.slice(0, 12)}...</Text></Table.Td>
                    <Table.Td><StatusBadge status={run.status} label={t(`run.status.${run.status}`)} /></Table.Td>
                    <Table.Td><Badge variant="outline" size="xs">{run.trigger}</Badge></Table.Td>
                    <Table.Td><Text size="xs">{formatDuration(run.durationMs)}</Text></Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        <IconClock size={12} />
                        <Text size="xs" c="dimmed">{formatRelative(run.queuedAt)}</Text>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Card>
      </Stack>

      <Divider my="md" />
      <Group gap="sm">
        {mission.tags.map((tag) => <Badge key={tag} variant="outline" size="sm">{tag}</Badge>)}
      </Group>
    </>
  )
}
