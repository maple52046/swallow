import { useEffect, useState } from 'react'
import {
  Card, Group, Text, Switch, Stack, ThemeIcon, ActionIcon, Tooltip, Badge,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconRobot, IconRefresh } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Agent } from '@/domain/platform/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { formatRelative } from '@/shared/utils/time'

function agentStatusColor(status: Agent['status']) {
  if (status === 'running') return 'green'
  if (status === 'error') return 'red'
  return 'gray'
}

export function AgentsPage() {
  const { platform } = useApp()
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)

  const load = () => {
    platform.listAgents.execute().then(setAgents).catch(() => null).finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleToggle = async (agent: Agent) => {
    await platform.enableDisableAgent.execute(agent.id, !agent.enabled)
    notifications.show({
      title: agent.enabled ? 'Agent disabled' : 'Agent enabled',
      message: agent.name,
      color: agent.enabled ? 'orange' : 'green',
    })
    load()
  }

  if (loading) return <LoadingState />

  const enabledCount = agents.filter((a) => a.enabled).length

  return (
    <>
      <PageHeader
        title={t('platform.agent.titlePlural')}
        subtitle={`${agents.length} agents · ${enabledCount} enabled`}
        actions={<ActionIcon variant="default" onClick={load}><IconRefresh size={16} /></ActionIcon>}
      />

      {agents.length === 0 ? (
        <EmptyState message={t('platform.agent.empty')} />
      ) : (
        <Stack gap="sm">
          {agents.map((agent) => (
            <Card key={agent.id} withBorder>
              <Group justify="space-between">
                <Group gap="sm">
                  <ThemeIcon variant="light" color={agent.enabled ? 'blue' : 'gray'} size="lg">
                    <IconRobot size={18} />
                  </ThemeIcon>
                  <Stack gap={0}>
                    <Group gap="xs">
                      <Text fw={500}>{agent.name}</Text>
                      <Badge size="xs" variant="outline">{agent.version}</Badge>
                      <Badge size="xs" color={agentStatusColor(agent.status)}>{agent.status}</Badge>
                    </Group>
                    <Text size="xs" c="dimmed">{agent.description}</Text>
                  </Stack>
                </Group>
                <Group gap="md">
                  {agent.capabilities && (
                    <Group gap={4}>
                      {agent.capabilities.map((cap) => (
                        <Badge key={cap} size="xs" variant="dot" color="blue">{cap}</Badge>
                      ))}
                    </Group>
                  )}
                  <Stack gap={2} ta="right">
                    <Text size="xs" c="dimmed">{t('platform.agent.heartbeat')}</Text>
                    <Text size="xs">{agent.lastHeartbeatAt ? formatRelative(agent.lastHeartbeatAt) : '—'}</Text>
                  </Stack>
                  <Tooltip label={agent.enabled ? t('platform.agent.disable') : t('platform.agent.enable')}>
                    <Switch checked={agent.enabled} onChange={() => void handleToggle(agent)} />
                  </Tooltip>
                </Group>
              </Group>
            </Card>
          ))}
        </Stack>
      )}
    </>
  )
}
