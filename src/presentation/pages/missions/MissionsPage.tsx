import { useEffect, useState, useCallback } from 'react'
import { Table, Group, Button, TextInput, Select, ActionIcon, Menu, Text, Stack, Badge } from '@mantine/core'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { notifications } from '@mantine/notifications'
import {
  IconPlus, IconSearch, IconPlayerPlay, IconPlayerPause, IconArchive, IconCopy,
  IconEdit, IconDots, IconRocket,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Mission } from '@/domain/mission/types'
import { t } from '@/presentation/app/i18n'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ModelLabel } from '@/presentation/components/ModelLabel'
import { formatRelative } from '@/shared/utils/time'
import type { Model } from '@/domain/platform/types'

export function MissionsPage() {
  const { missions, platform } = useApp()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [data, setData] = useState<Mission[] | null>(null)
  const [modelsById, setModelsById] = useState<Map<string, Model>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const statusFilter = searchParams.get('status') ?? ''
  const searchQuery = searchParams.get('q') ?? ''

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const [result, models] = await Promise.all([
        missions.list.execute({
          status: statusFilter as Mission['status'] || undefined,
          search: searchQuery || undefined,
        }),
        platform.listModels.execute(),
      ])
      setData(result)
      setModelsById(new Map(models.map((m) => [m.id, m])))
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [missions.list, platform.listModels, statusFilter, searchQuery])

  useEffect(() => { void load() }, [load])

  const handleRunNow = async (id: string) => {
    try {
      const run = await missions.runNow.execute(id)
      notifications.show({ title: 'Run started', message: `Run ${run.id.slice(0, 8)}... created`, color: 'green' })
      navigate(`/deprecated/runs/${run.id}`)
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  const handlePause = async (id: string) => {
    try {
      await missions.pause.execute(id)
      notifications.show({ title: 'Mission paused', message: '', color: 'yellow' })
      void load()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  const handleResume = async (id: string) => {
    try {
      await missions.resume.execute(id)
      notifications.show({ title: 'Mission resumed', message: '', color: 'green' })
      void load()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  const handleArchive = async (id: string) => {
    try {
      await missions.archive.execute(id)
      notifications.show({ title: 'Mission archived', message: '', color: 'gray' })
      void load()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  const handleClone = async (id: string) => {
    try {
      const cloned = await missions.clone.execute(id)
      notifications.show({ title: 'Mission cloned', message: `Created "${cloned.name}"`, color: 'blue' })
      navigate(`/deprecated/missions/${cloned.id}`)
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  return (
    <>
      <PageHeader
        title={t('mission.titlePlural')}
        subtitle={data ? `${data.length} missions` : undefined}
        actions={
          <Button leftSection={<IconPlus size={16} />} onClick={() => navigate('/deprecated/missions/new')}>
            {t('mission.create')}
          </Button>
        }
      />

      <Group mb="md" gap="sm">
        <TextInput
          placeholder={t('common.search')}
          leftSection={<IconSearch size={14} />}
          value={searchQuery}
          onChange={(e) => setSearchParams((p) => { const n = new URLSearchParams(p); n.set('q', e.target.value); return n })}
          style={{ flex: 1 }}
        />
        <Select
          placeholder={t('common.filter') + ': ' + t('common.status')}
          clearable
          data={[
            { value: 'active', label: t('mission.status.active') },
            { value: 'paused', label: t('mission.status.paused') },
            { value: 'draft', label: t('mission.status.draft') },
            { value: 'archived', label: t('mission.status.archived') },
          ]}
          value={statusFilter || null}
          onChange={(v) =>
            setSearchParams((p) => {
              const n = new URLSearchParams(p)
              if (v) n.set('status', v)
              else n.delete('status')
              return n
            })
          }
          w={160}
        />
      </Group>

      {loading ? (
        <LoadingState rows={5} height={50} />
      ) : error ? (
        <ErrorState message={error} onRetry={load} />
      ) : !data?.length ? (
        <EmptyState
          title={t('mission.empty')}
          action={{ label: t('mission.create'), onClick: () => navigate('/deprecated/missions/new') }}
          icon={<IconRocket size={24} />}
        />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('common.name')}</Table.Th>
              <Table.Th>{t('common.status')}</Table.Th>
              <Table.Th>{t('mission.trigger.manual')}</Table.Th>
              <Table.Th>{t('mission.model')}</Table.Th>
              <Table.Th>{t('mission.runCount')}</Table.Th>
              <Table.Th>{t('mission.lastRunAt')}</Table.Th>
              <Table.Th>{t('common.actions')}</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.map((m) => (
              <Table.Tr key={m.id}>
                <Table.Td>
                  <Stack gap={2}>
                    <Text
                      size="sm" fw={500} style={{ cursor: 'pointer' }}
                      c="blue" onClick={() => navigate(`/deprecated/missions/${m.id}`)}
                    >
                      {m.name}
                    </Text>
                    <Group gap={4}>
                      {m.tags.slice(0, 3).map((tag) => (
                        <Badge key={tag} size="xs" variant="outline">{tag}</Badge>
                      ))}
                    </Group>
                  </Stack>
                </Table.Td>
                <Table.Td><StatusBadge status={m.status} label={t(`mission.status.${m.status}`)} /></Table.Td>
                <Table.Td>
                  <Badge variant="outline" size="xs">{m.trigger}</Badge>
                  {m.schedule && <Text size="xs" c="dimmed" mt={2}>{m.schedule}</Text>}
                </Table.Td>
                <Table.Td><ModelLabel modelId={m.modelId} modelsById={modelsById} /></Table.Td>
                <Table.Td><Text size="sm">{m.runCount}</Text></Table.Td>
                <Table.Td><Text size="xs" c="dimmed">{formatRelative(m.lastRunAt)}</Text></Table.Td>
                <Table.Td>
                  <Group gap="xs">
                    <ActionIcon
                      variant="subtle" size="sm" color="blue"
                      title={t('mission.actions.runNow')}
                      disabled={m.status === 'archived' || m.status === 'paused'}
                      onClick={() => void handleRunNow(m.id)}
                    >
                      <IconPlayerPlay size={14} />
                    </ActionIcon>
                    <Menu position="bottom-end" withArrow>
                      <Menu.Target>
                        <ActionIcon variant="subtle" size="sm"><IconDots size={14} /></ActionIcon>
                      </Menu.Target>
                      <Menu.Dropdown>
                        <Menu.Item leftSection={<IconEdit size={14} />} onClick={() => navigate(`/deprecated/missions/${m.id}`)}>
                          {t('common.viewDetails')}
                        </Menu.Item>
                        {m.status === 'active' && (
                          <Menu.Item leftSection={<IconPlayerPause size={14} />} onClick={() => void handlePause(m.id)}>
                            {t('mission.actions.pause')}
                          </Menu.Item>
                        )}
                        {m.status === 'paused' && (
                          <Menu.Item leftSection={<IconPlayerPlay size={14} />} onClick={() => void handleResume(m.id)}>
                            {t('mission.actions.resume')}
                          </Menu.Item>
                        )}
                        <Menu.Item leftSection={<IconCopy size={14} />} onClick={() => void handleClone(m.id)}>
                          {t('mission.actions.clone')}
                        </Menu.Item>
                        {m.status !== 'archived' && (
                          <>
                            <Menu.Divider />
                            <Menu.Item leftSection={<IconArchive size={14} />} color="red" onClick={() => void handleArchive(m.id)}>
                              {t('mission.actions.archive')}
                            </Menu.Item>
                          </>
                        )}
                      </Menu.Dropdown>
                    </Menu>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
