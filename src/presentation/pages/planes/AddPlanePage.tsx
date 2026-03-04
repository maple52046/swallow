import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Card, Stack, Button, TextInput, Select, Group, ActionIcon, TagsInput,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconArrowLeft } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'

export function AddPlanePage() {
  const { planes } = useApp()
  const navigate = useNavigate()

  const [planeType, setPlaneType] = useState<'kubernetes' | 'slurm'>('kubernetes')
  const [name, setName] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [authRef, setAuthRef] = useState('')
  const [labels, setLabels] = useState<string[]>([])
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    if (!name || !endpoint) return
    setSaving(true)
    try {
      const plane = await planes.register.execute({
        type: planeType,
        name,
        endpointRef: endpoint,
        authRef,
        labels,
      })
      notifications.show({ title: 'Plane registered', message: plane.name, color: 'green' })
      navigate('/planes')
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <Group mb="md">
        <ActionIcon variant="subtle" onClick={() => navigate('/planes')}><IconArrowLeft size={16} /></ActionIcon>
      </Group>
      <PageHeader title={t('plane.register.title')} subtitle="Connect a Kubernetes or Slurm cluster to DC Dashboard" />

      <Card withBorder maw={600}>
        <Stack gap="md">
          <Select
            label={t('plane.register.typeLabel')}
            data={[
              { value: 'kubernetes', label: t('plane.type.kubernetes') },
              { value: 'slurm', label: t('plane.type.slurm') },
            ]}
            value={planeType}
            onChange={(v) => v && setPlaneType(v as 'kubernetes' | 'slurm')}
            required
          />
          <TextInput
            label={t('plane.register.nameLabel')}
            placeholder={planeType === 'kubernetes' ? 'prod-gpu-cluster' : 'hpc-slurm-01'}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <TextInput
            label={t('plane.register.endpointLabel')}
            description={planeType === 'kubernetes' ? 'Kubeconfig secret reference, e.g. vault://k8s/prod' : 'Slurm REST API endpoint reference'}
            placeholder={planeType === 'kubernetes' ? 'vault://k8s/prod-kubeconfig' : 'vault://slurm/api-token'}
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            required
          />
          <TextInput
            label={t('plane.register.authLabel')}
            description="Vault reference for authentication credentials"
            placeholder="vault://secrets/plane-auth"
            value={authRef}
            onChange={(e) => setAuthRef(e.target.value)}
          />
          <TagsInput
            label={t('plane.register.labelsLabel')}
            description="Labels as strings (e.g. env=prod, region=us-east)"
            placeholder="env=prod"
            value={labels}
            onChange={setLabels}
          />
          <Group justify="flex-end" mt="md">
            <Button variant="default" onClick={() => navigate('/planes')}>{t('common.cancel')}</Button>
            <Button onClick={() => void handleSave()} loading={saving} disabled={!name || !endpoint}>
              {t('plane.actions.register')}
            </Button>
          </Group>
        </Stack>
      </Card>
    </>
  )
}
