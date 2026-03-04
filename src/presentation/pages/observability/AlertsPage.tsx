import { useEffect, useState, useCallback } from 'react'
import {
  Group, Select, TextInput, ActionIcon, Tooltip, Text, Stack,
  Card, ThemeIcon, Collapse, Table, Badge,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { IconSearch, IconRefresh, IconCheck, IconEye, IconSparkles, IconAlertCircle, IconInfoCircle } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Alert, AlertSeverity, AlertStatus } from '@/domain/alert/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatRelative } from '@/shared/utils/time'

function severityIcon(severity: AlertSeverity) {
  if (severity === 'critical') return <ThemeIcon color="red" size="sm"><IconAlertCircle size={12} /></ThemeIcon>
  if (severity === 'warning') return <ThemeIcon color="yellow" size="sm"><IconAlertCircle size={12} /></ThemeIcon>
  return <ThemeIcon color="blue" size="sm"><IconInfoCircle size={12} /></ThemeIcon>
}

export function AlertsPage() {
  const { observability } = useApp()
  const navigate = useNavigate()

  const [items, setItems] = useState<Alert[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [severity, setSeverity] = useState<AlertSeverity | null>(null)
  const [status, setStatus] = useState<AlertStatus | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const result = await observability.listAlerts.execute({
        severity: severity ?? undefined,
        status: status ?? undefined,
      })
      setItems(result)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [observability.listAlerts, severity, status])

  useEffect(() => {
    void load()
    const iv = setInterval(() => void load(), 5000)
    return () => clearInterval(iv)
  }, [load])

  const handleAck = async (id: string) => {
    await observability.ackAlert.execute(id)
    notifications.show({ title: 'Alert acknowledged', message: id, color: 'green' })
    void load()
  }

  const handleResolve = async (id: string) => {
    await observability.resolveAlert.execute(id)
    notifications.show({ title: 'Alert resolved', message: id, color: 'teal' })
    void load()
  }

  const handleCreateMission = async (alert: Alert) => {
    const mission = await observability.createMissionFromAlert.execute({
      alertId: alert.id,
      goal: alert.suggestedMissionGoal ?? `Investigate and resolve alert: ${alert.title}. ${alert.message}`,
      target: alert.suggestedTarget ?? (alert.relatedAssetIds[0] ?? 'unknown'),
      plugins: alert.suggestedPlugins ?? ['ssh', 'nvidia-smi'],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'blue' })
    navigate(`/missions/${mission.id}`)
  }

  const filtered = items.filter((a) =>
    !search || a.title.toLowerCase().includes(search.toLowerCase()) || a.message.toLowerCase().includes(search.toLowerCase()),
  )

  const criticalCount = items.filter((a) => a.severity === 'critical' && a.status === 'active').length
  const warningCount = items.filter((a) => a.severity === 'warning' && a.status === 'active').length

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title={t('alert.titlePlural')}
        subtitle={`${criticalCount} critical · ${warningCount} warning`}
        actions={
          <Tooltip label="Refresh">
            <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
          </Tooltip>
        }
      />

      <Group gap="sm" mb="md">
        <TextInput
          placeholder={t('common.search')}
          leftSection={<IconSearch size={14} />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          w={240}
        />
        <Select
          placeholder="Severity"
          data={[
            { value: 'critical', label: t('alert.severity.critical') },
            { value: 'warning', label: t('alert.severity.warning') },
            { value: 'info', label: t('alert.severity.info') },
          ]}
          value={severity}
          onChange={(v) => setSeverity(v as AlertSeverity | null)}
          clearable
          w={130}
        />
        <Select
          placeholder="Status"
          data={[
            { value: 'active', label: t('alert.status.active') },
            { value: 'acknowledged', label: t('alert.status.acknowledged') },
            { value: 'resolved', label: t('alert.status.resolved') },
          ]}
          value={status}
          onChange={(v) => setStatus(v as AlertStatus | null)}
          clearable
          w={150}
        />
      </Group>

      {filtered.length === 0 ? (
        <EmptyState message={t('alert.empty')} icon={<IconCheck size={32} color="var(--mantine-color-green-5)" />} />
      ) : (
        <Stack gap="xs">
          {filtered.map((alert) => (
            <Card key={alert.id} withBorder p="sm">
              <Group justify="space-between" wrap="nowrap">
                <Group gap="sm" wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
                  {severityIcon(alert.severity)}
                  <Stack gap={2} style={{ flex: 1, minWidth: 0 }}>
                    <Group gap="xs">
                      <Text size="sm" fw={500}>{alert.title}</Text>
                      <StatusBadge status={alert.status} />
                      <Badge size="xs" color={alert.severity === 'critical' ? 'red' : alert.severity === 'warning' ? 'yellow' : 'blue'}>
                        {alert.severity}
                      </Badge>
                    </Group>
                    <Text size="xs" c="dimmed" lineClamp={expandedId === alert.id ? undefined : 1}>{alert.message}</Text>
                    {alert.relatedAssetIds.length > 0 && (
                      <Group gap="xs">
                        {alert.relatedAssetIds.slice(0, 4).map((a) => <Badge key={a} size="xs" variant="outline">{a}</Badge>)}
                        {alert.relatedAssetIds.length > 4 && <Text size="xs" c="dimmed">+{alert.relatedAssetIds.length - 4} more</Text>}
                      </Group>
                    )}
                  </Stack>
                </Group>
                <Group gap="xs" wrap="nowrap" ml="sm">
                  <Text size="xs" c="dimmed" style={{ whiteSpace: 'nowrap' }}>{formatRelative(alert.createdAt)}</Text>
                  <Tooltip label="Toggle details">
                    <ActionIcon variant="subtle" size="sm" onClick={() => setExpandedId(expandedId === alert.id ? null : alert.id)}>
                      <IconEye size={14} />
                    </ActionIcon>
                  </Tooltip>
                  {alert.status === 'active' && (
                    <Tooltip label={t('alert.actions.ack')}>
                      <ActionIcon variant="subtle" size="sm" color="yellow" onClick={() => void handleAck(alert.id)}>
                        <IconCheck size={14} />
                      </ActionIcon>
                    </Tooltip>
                  )}
                  {alert.status !== 'resolved' && (
                    <Tooltip label={t('alert.actions.resolve')}>
                      <ActionIcon variant="subtle" size="sm" color="green" onClick={() => void handleResolve(alert.id)}>
                        <IconCheck size={14} />
                      </ActionIcon>
                    </Tooltip>
                  )}
                  {alert.status !== 'resolved' && (
                    <Tooltip label={t('alert.actions.createMission')}>
                      <ActionIcon variant="subtle" size="sm" color="blue" onClick={() => void handleCreateMission(alert)}>
                        <IconSparkles size={14} />
                      </ActionIcon>
                    </Tooltip>
                  )}
                </Group>
              </Group>
              <Collapse in={expandedId === alert.id}>
                <Table mt="sm" fz="xs" withRowBorders={false}>
                  <Table.Tbody>
                    <Table.Tr>
                      <Table.Td c="dimmed">ID</Table.Td>
                      <Table.Td ff="mono">{alert.id}</Table.Td>
                      <Table.Td c="dimmed">Triggered</Table.Td>
                      <Table.Td>{new Date(alert.createdAt).toLocaleString()}</Table.Td>
                    </Table.Tr>
                    {alert.suggestedMissionGoal && (
                      <Table.Tr>
                        <Table.Td c="dimmed">{t('alert.suggestedMission')}</Table.Td>
                        <Table.Td colSpan={3}>{alert.suggestedMissionGoal}</Table.Td>
                      </Table.Tr>
                    )}
                  </Table.Tbody>
                </Table>
              </Collapse>
            </Card>
          ))}
        </Stack>
      )}
    </>
  )
}
