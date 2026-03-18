import { useEffect, useState } from 'react'
import {
  Group, Text, Button, Card, Stack, ThemeIcon, ActionIcon, Tooltip, Badge,
} from '@mantine/core'
import { modals } from '@mantine/modals'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { IconPower, IconRefresh, IconAlertCircle, IconSparkles, IconServer } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Host } from '@/domain/asset/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'

export function IPMIPage() {
  const { datacenter, missions, platform } = useApp()
  const navigate = useNavigate()

  const [hosts, setHosts] = useState<Host[]>([])
  const [loading, setLoading] = useState(true)
  const [defaultModelId, setDefaultModelId] = useState('model-gpt4o')

  useEffect(() => {
    platform.listModels.execute().then((models) => {
      const fallback = models.find((m) => m.isDefault) ?? models[0]
      if (fallback) setDefaultModelId(fallback.id)
    }).catch(() => null)
  }, [platform.listModels])

  useEffect(() => {
    datacenter.listHosts.execute({}).then(setHosts).catch(() => null).finally(() => setLoading(false))
  }, [datacenter.listHosts])

  const hostsWithBMC = hosts.filter((h) => h.bmcAddress)

  const handleAction = (host: Host, action: string) => {
    modals.openConfirmModal({
      title: `${action} — ${host.name}`,
      children: <Text size="sm">Are you sure you want to {action.toLowerCase()} {host.name} via IPMI?</Text>,
      labels: { confirm: action, cancel: 'Cancel' },
      confirmProps: { color: action === 'Power Off' ? 'red' : 'blue' },
      onConfirm: async () => {
        const mission = await missions.create.execute({
          name: `IPMI ${action} — ${host.name}`,
          goal: `Execute IPMI ${action.toLowerCase()} on host ${host.name} (BMC: ${host.bmcAddress}).`,
          modelId: defaultModelId,
          target: host.id,
          trigger: 'manual',
          plan: { steps: [{ id: 'step-1', order: 1, name: `IPMI ${action}`, description: `Execute ${action} via BMC`, plugin: 'ipmi', action: action.toLowerCase().replace(' ', '-'), parameters: { address: host.bmcAddress } }], estimatedDurationSeconds: 30 },
          permissions: { allowedPlugins: ['ipmi'], allowedTargets: [host.id], guardrails: [] },
          tags: ['ipmi', action.toLowerCase()],
        })
        notifications.show({ title: `Mission created: ${action}`, message: mission.name, color: 'blue' })
        navigate(`/missions/${mission.id}`)
      },
    })
  }

  const handleSensorsMission = async (host: Host) => {
    const mission = await missions.create.execute({
      name: `IPMI Sensors — ${host.name}`,
      goal: `Collect all IPMI sensor readings from ${host.name} (BMC: ${host.bmcAddress}). Report temperature, power, fan speeds, and voltage.`,
      modelId: defaultModelId,
      target: host.id,
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 60 },
      permissions: { allowedPlugins: ['ipmi', 'ssh'], allowedTargets: [host.id], guardrails: ['read-only'] },
      tags: ['ipmi', 'sensors'],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'green' })
    navigate(`/missions/${mission.id}`)
  }

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader
        title={t('ipmi.title')}
        subtitle={`${hostsWithBMC.length} hosts with BMC`}
      />

      {hostsWithBMC.length === 0 ? (
        <EmptyState message={t('ipmi.empty')} />
      ) : (
        <Stack gap="sm">
          {hostsWithBMC.map((host) => (
            <Card key={host.id} withBorder>
              <Group justify="space-between">
                <Group gap="sm">
                  <ThemeIcon size="lg" variant="light" color={host.status === 'healthy' ? 'green' : 'red'}>
                    <IconServer size={20} />
                  </ThemeIcon>
                  <Stack gap={0}>
                    <Group gap="xs">
                      <Text fw={500}>{host.name}</Text>
                      <Badge size="xs" variant="outline" color={host.status === 'healthy' ? 'green' : 'red'}>
                        {host.status}
                      </Badge>
                    </Group>
                    <Text size="xs" c="dimmed">BMC: <code>{host.bmcAddress}</code></Text>
                  </Stack>
                </Group>
                <Group gap="xs">
                  <Tooltip label={t('ipmi.actions.sensors')}>
                    <Button size="xs" variant="light" leftSection={<IconAlertCircle size={12} />}
                      onClick={() => void handleSensorsMission(host)}>
                      Sensors
                    </Button>
                  </Tooltip>
                  <Tooltip label={t('ipmi.actions.reboot')}>
                    <ActionIcon variant="light" color="orange" onClick={() => handleAction(host, 'Reboot')}>
                      <IconRefresh size={16} />
                    </ActionIcon>
                  </Tooltip>
                  <Tooltip label={t('ipmi.actions.powerOn')}>
                    <ActionIcon variant="light" color="green" onClick={() => handleAction(host, 'Power On')}>
                      <IconPower size={16} />
                    </ActionIcon>
                  </Tooltip>
                  <Tooltip label={t('ipmi.actions.powerOff')}>
                    <ActionIcon variant="light" color="red" onClick={() => handleAction(host, 'Power Off')}>
                      <IconPower size={16} />
                    </ActionIcon>
                  </Tooltip>
                  <Tooltip label={t('ipmi.createMission')}>
                    <ActionIcon variant="light" color="blue" onClick={() => void handleSensorsMission(host)}>
                      <IconSparkles size={16} />
                    </ActionIcon>
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
