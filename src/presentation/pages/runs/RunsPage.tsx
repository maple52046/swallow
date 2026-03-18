import { useEffect, useState, useCallback } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Table, Badge, Group, Select, TextInput, Button, ActionIcon, Tooltip, Text, Stack,
} from '@mantine/core'
import { IconSearch, IconX, IconEye, IconRefresh } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Run, RunStatus } from '@/domain/run/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ModelLabel } from '@/presentation/components/ModelLabel'
import { resolveModelInfo } from '@/presentation/components/modelLabelUtils'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatRelative, formatDuration } from '@/shared/utils/time'
import type { Model } from '@/domain/platform/types'

export function RunsPage() {
  const { runs, platform } = useApp()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [items, setItems] = useState<Run[]>([])
  const [modelsById, setModelsById] = useState<Map<string, Model>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const status = (searchParams.get('status') as RunStatus | null) ?? undefined
  const missionId = searchParams.get('missionId') ?? undefined
  const search = searchParams.get('q') ?? ''

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [result, models] = await Promise.all([
        runs.list.execute({ status, missionId }),
        platform.listModels.execute(),
      ])
      const modelMap = new Map(models.map((m) => [m.id, m]))
      setModelsById(modelMap)
      const filtered = search
        ? result.filter((r) =>
            r.id.includes(search) ||
            resolveModelInfo(r.modelId, modelMap).searchText.includes(search.toLowerCase()) ||
            r.target.toLowerCase().includes(search.toLowerCase()) ||
            r.goal.toLowerCase().includes(search.toLowerCase()),
          )
        : result
      setItems(filtered)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [runs.list, platform.listModels, status, missionId, search])

  useEffect(() => {
    void load()
    const iv = setInterval(() => void load(), 5000)
    return () => clearInterval(iv)
  }, [load])

  const setParam = (key: string, value: string | undefined) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      if (value) next.set(key, value)
      else next.delete(key)
      return next
    })
  }

  if (loading && items.length === 0) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title={t('run.titlePlural')}
        subtitle={`${items.length} runs`}
        actions={
          <Tooltip label="Refresh">
            <ActionIcon variant="default" onClick={() => void load()}>
              <IconRefresh size={16} />
            </ActionIcon>
          </Tooltip>
        }
      />

      <Stack gap="sm" mb="md">
        <Group>
          <TextInput
            placeholder={t('common.search')}
            leftSection={<IconSearch size={14} />}
            value={search}
            onChange={(e) => setParam('q', e.target.value || undefined)}
            rightSection={search ? <ActionIcon size="xs" variant="subtle" onClick={() => setParam('q', undefined)}><IconX size={12} /></ActionIcon> : null}
            w={280}
          />
          <Select
            placeholder="All statuses"
            data={[
              { value: 'queued', label: t('run.status.queued') },
              { value: 'running', label: t('run.status.running') },
              { value: 'succeeded', label: t('run.status.succeeded') },
              { value: 'failed', label: t('run.status.failed') },
              { value: 'canceled', label: t('run.status.canceled') },
            ]}
            value={status ?? null}
            onChange={(v) => setParam('status', v ?? undefined)}
            clearable
            w={160}
          />
          {(status || missionId || search) && (
            <Button variant="subtle" size="sm" onClick={() => setSearchParams({})}>
              {t('common.clear')}
            </Button>
          )}
        </Group>
      </Stack>

      {items.length === 0 ? (
        <EmptyState message={t('run.empty')} />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>ID</Table.Th>
              <Table.Th>{t('common.status')}</Table.Th>
              <Table.Th>Trigger</Table.Th>
              <Table.Th>Model</Table.Th>
              <Table.Th>Target</Table.Th>
              <Table.Th>Goal</Table.Th>
              <Table.Th>{t('run.duration')}</Table.Th>
              <Table.Th>{t('run.queuedAt')}</Table.Th>
              <Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {items.map((run) => (
              <Table.Tr key={run.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/runs/${run.id}`)}>
                <Table.Td>
                  <Text size="xs" c="dimmed" ff="mono">{run.id.slice(0, 8)}</Text>
                </Table.Td>
                <Table.Td><StatusBadge status={run.status} /></Table.Td>
                <Table.Td>
                  <Badge size="xs" variant="outline">{run.trigger}</Badge>
                </Table.Td>
                <Table.Td>
                  <ModelLabel modelId={run.modelId} modelsById={modelsById} />
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="mono">{run.target}</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" lineClamp={1} maw={220}>{run.goal}</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">
                    {run.startedAt && run.completedAt
                      ? formatDuration(new Date(run.completedAt).getTime() - new Date(run.startedAt).getTime())
                      : run.startedAt
                        ? formatDuration(Date.now() - new Date(run.startedAt).getTime())
                        : '—'}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">{formatRelative(run.queuedAt)}</Text>
                </Table.Td>
                <Table.Td>
                  <ActionIcon variant="subtle" size="sm" onClick={(e) => { e.stopPropagation(); navigate(`/runs/${run.id}`) }}>
                    <IconEye size={14} />
                  </ActionIcon>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
