import { useEffect, useState, useRef, useCallback } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  Group, Text, Card, Badge, Button, Tabs, Table, Anchor,
  ActionIcon, Tooltip, Switch, ScrollArea, Code, ThemeIcon,
  Progress, Divider, Box,
} from '@mantine/core'
import { modals } from '@mantine/modals'
import { notifications } from '@mantine/notifications'
import {
  IconArrowLeft, IconX, IconRefresh, IconDownload, IconPlus,
  IconCheck, IconLoader, IconClock, IconAlertCircle,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Run } from '@/domain/run/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ModelLabel } from '@/presentation/components/ModelLabel'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatRelative, formatDuration, formatDateTime } from '@/shared/utils/time'
import type { Model } from '@/domain/platform/types'

function stepStatusIcon(status: string) {
  if (status === 'succeeded') return <ThemeIcon color="green" size="sm" radius="xl"><IconCheck size={12} /></ThemeIcon>
  if (status === 'running') return <ThemeIcon color="blue" size="sm" radius="xl"><IconLoader size={12} /></ThemeIcon>
  if (status === 'failed') return <ThemeIcon color="red" size="sm" radius="xl"><IconAlertCircle size={12} /></ThemeIcon>
  return <ThemeIcon color="gray" variant="outline" size="sm" radius="xl"><IconClock size={12} /></ThemeIcon>
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { runs, missions, platform } = useApp()

  const [run, setRun] = useState<Run | null>(null)
  const [modelsById, setModelsById] = useState<Map<string, Model>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [autoScroll, setAutoScroll] = useState(true)
  const [canceling, setCanceling] = useState(false)
  const [rerunning, setRerunning] = useState(false)
  const logsRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [data, models] = await Promise.all([
        runs.get.execute(id),
        platform.listModels.execute(),
      ])
      setRun(data)
      setModelsById(new Map(models.map((m) => [m.id, m])))
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [id, runs.get, platform.listModels])

  useEffect(() => {
    void load()
    const iv = setInterval(() => void load(), 2000)
    return () => clearInterval(iv)
  }, [load])

  useEffect(() => {
    if (autoScroll && logsRef.current) {
      logsRef.current.scrollTop = logsRef.current.scrollHeight
    }
  }, [run?.logs, autoScroll])

  const handleCancel = () => {
    modals.openConfirmModal({
      title: t('run.actions.cancel'),
      children: <Text size="sm">{t('run.cancelConfirm')}</Text>,
      labels: { confirm: t('run.actions.cancel'), cancel: t('common.cancel') },
      confirmProps: { color: 'red' },
      onConfirm: async () => {
        if (!id) return
        setCanceling(true)
        try {
          await runs.cancel.execute(id)
          notifications.show({ title: 'Run canceled', message: id, color: 'orange' })
          void load()
        } finally {
          setCanceling(false)
        }
      },
    })
  }

  const handleRerun = async () => {
    if (!id) return
    setRerunning(true)
    try {
      const newRun = await runs.rerun.execute(id)
      notifications.show({ title: 'Rerun started', message: newRun.id, color: 'blue' })
      navigate(`/deprecated/runs/${newRun.id}`)
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setRerunning(false)
    }
  }

  const handleFollowUp = async () => {
    if (!run) return
    const mission = await missions.clone.execute(run.missionId)
    navigate(`/deprecated/missions/${mission.id}`)
  }

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />
  if (!run) return <ErrorState message={t('error.notFound')} />

  const completedSteps = run.steps.filter((s) => s.status === 'succeeded').length
  const totalSteps = run.steps.length
  const progressPct = totalSteps > 0 ? Math.round((completedSteps / totalSteps) * 100) : 0
  const duration = run.startedAt
    ? run.completedAt
      ? new Date(run.completedAt).getTime() - new Date(run.startedAt).getTime()
      : Date.now() - new Date(run.startedAt).getTime()
    : null

  const isActive = run.status === 'queued' || run.status === 'running'

  return (
    <>
      <Group mb="md">
        <ActionIcon variant="subtle" onClick={() => navigate('/deprecated/runs')}>
          <IconArrowLeft size={16} />
        </ActionIcon>
      </Group>

      <PageHeader
        title={`Run ${run.id.slice(0, 8)}`}
        subtitle={run.goal}
        actions={
          <Group gap="xs">
            <Tooltip label="Refresh">
              <ActionIcon variant="default" onClick={() => void load()}>
                <IconRefresh size={16} />
              </ActionIcon>
            </Tooltip>
            {isActive && (
              <Button variant="outline" color="red" size="sm" leftSection={<IconX size={14} />}
                onClick={handleCancel} loading={canceling}>
                {t('run.actions.cancel')}
              </Button>
            )}
            {!isActive && (
              <>
                <Button variant="outline" size="sm" onClick={() => void handleRerun()} loading={rerunning}>
                  {t('run.actions.rerun')}
                </Button>
                <Button variant="light" size="sm" leftSection={<IconPlus size={14} />}
                  onClick={() => void handleFollowUp()}>
                  {t('run.actions.followUp')}
                </Button>
              </>
            )}
          </Group>
        }
      />

      <Group gap="xs" mb="xl">
        <StatusBadge status={run.status} />
        <Badge variant="outline" size="sm">{run.trigger}</Badge>
        <Group gap={6}>
          <Text size="sm" c="dimmed">Model:</Text>
          <ModelLabel modelId={run.modelId} modelsById={modelsById} />
        </Group>
        <Text size="sm" c="dimmed">Target: <Code>{run.target}</Code></Text>
        {duration && <Text size="sm" c="dimmed">Duration: <b>{formatDuration(duration)}</b></Text>}
        <Text size="sm" c="dimmed">{t('run.queuedAt')}: {formatRelative(run.queuedAt)}</Text>
      </Group>

      <Text size="sm" mb="md" c="dimmed">
        Mission: <Anchor size="sm" onClick={() => navigate(`/deprecated/missions/${run.missionId}`)}>{run.missionName}</Anchor>
      </Text>

      {isActive && totalSteps > 0 && (
        <Card withBorder mb="md">
          <Group justify="space-between" mb="xs">
            <Text size="sm" fw={500}>Progress</Text>
            <Text size="sm" c="dimmed">{completedSteps}/{totalSteps} steps</Text>
          </Group>
          <Progress value={progressPct} size="sm" animated={run.status === 'running'} />
        </Card>
      )}

      <Tabs defaultValue="steps">
        <Tabs.List mb="md">
          <Tabs.Tab value="steps">{t('run.steps')} ({totalSteps})</Tabs.Tab>
          <Tabs.Tab value="logs">{t('run.logs')} ({run.logs.length} lines)</Tabs.Tab>
          <Tabs.Tab value="artifacts">{t('run.artifacts')} ({run.artifacts.length})</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="steps">
          {totalSteps === 0 ? (
            <Text size="sm" c="dimmed">No steps defined.</Text>
          ) : (
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>#</Table.Th>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                  <Table.Th>Started</Table.Th>
                  <Table.Th>Duration</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {run.steps.map((step) => (
                  <Table.Tr key={step.id}>
                    <Table.Td><Text size="sm" c="dimmed">{step.order}</Text></Table.Td>
                    <Table.Td>
                      <Group gap="xs">
                        {stepStatusIcon(step.status)}
                        <Text size="sm">{step.name}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="xs" color={
                        step.status === 'succeeded' ? 'green' :
                        step.status === 'running' ? 'blue' :
                        step.status === 'failed' ? 'red' : 'gray'
                      }>{step.status}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs" c="dimmed">{step.startedAt ? formatDateTime(step.startedAt) : '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs">{step.durationMs ? formatDuration(step.durationMs) : '—'}</Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="logs">
          <Group justify="space-between" mb="xs">
            <Text size="sm" fw={500}>{t('run.logs')}</Text>
            <Switch
              label={t('run.autoScroll')}
              size="xs"
              checked={autoScroll}
              onChange={(e) => setAutoScroll(e.currentTarget.checked)}
            />
          </Group>
          <ScrollArea h={400} viewportRef={logsRef} styles={{ viewport: { fontFamily: 'monospace' } }}>
            <Box p="xs" style={{ backgroundColor: 'var(--mantine-color-dark-8)', borderRadius: 'var(--mantine-radius-sm)' }}>
              {run.logs.length === 0 ? (
                <Text size="xs" c="dimmed">{t('run.noLogs')}</Text>
              ) : (
                run.logs.map((line, i) => (
                  <Text key={i} size="xs" ff="mono" c="green.3" style={{ whiteSpace: 'pre-wrap' }}>{line}</Text>
                ))
              )}
            </Box>
          </ScrollArea>
        </Tabs.Panel>

        <Tabs.Panel value="artifacts">
          {run.artifacts.length === 0 ? (
            <Text size="sm" c="dimmed">{t('run.noArtifacts')}</Text>
          ) : (
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>{t('common.type')}</Table.Th>
                  <Table.Th>Size</Table.Th>
                  <Table.Th>Created</Table.Th>
                  <Table.Th></Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {run.artifacts.map((artifact) => (
                  <Table.Tr key={artifact.id}>
                    <Table.Td><Text size="sm">{artifact.name}</Text></Table.Td>
                    <Table.Td><Badge size="xs" variant="outline">{artifact.type}</Badge></Table.Td>
                    <Table.Td><Text size="sm">{formatBytes(artifact.sizeBytes)}</Text></Table.Td>
                    <Table.Td><Text size="sm" c="dimmed">{formatRelative(artifact.createdAt)}</Text></Table.Td>
                    <Table.Td>
                      <ActionIcon variant="subtle" size="sm" component="a" href={artifact.url} target="_blank">
                        <IconDownload size={14} />
                      </ActionIcon>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>
      </Tabs>

      <Divider my="xl" />

      <Text size="xs" c="dimmed">
        Queued: {formatDateTime(run.queuedAt)}
        {run.startedAt && ` · Started: ${formatDateTime(run.startedAt)}`}
        {run.completedAt && ` · Completed: ${formatDateTime(run.completedAt)}`}
      </Text>
    </>
  )
}
