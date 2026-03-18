import { useEffect, useState } from 'react'
import {
  Table, Badge, Group, Text, Tabs, Card, Stack, ThemeIcon, Button,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { IconPackage, IconSettings, IconHistory, IconSparkles } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { ProvisioningImage, ProvisioningProfile, ProvisioningJob } from '@/domain/platform/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { formatRelative } from '@/shared/utils/time'

export function ProvisioningPage() {
  const { datacenter, missions, platform } = useApp()
  const navigate = useNavigate()

  const [images, setImages] = useState<ProvisioningImage[]>([])
  const [profiles, setProfiles] = useState<ProvisioningProfile[]>([])
  const [jobs, setJobs] = useState<ProvisioningJob[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [defaultModelId, setDefaultModelId] = useState('model-gpt4o')

  useEffect(() => {
    platform.listModels.execute().then((models) => {
      const fallback = models.find((m) => m.isDefault) ?? models[0]
      if (fallback) setDefaultModelId(fallback.id)
    }).catch(() => null)
  }, [platform.listModels])

  useEffect(() => {
    Promise.all([
      datacenter.listProvisioningImages.execute(),
      datacenter.listProvisioningProfiles.execute(),
      datacenter.listProvisioningJobs.execute(),
    ]).then(([imgs, profs, js]) => {
      setImages(imgs)
      setProfiles(profs)
      setJobs(js)
    }).catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [datacenter.listProvisioningImages, datacenter.listProvisioningProfiles, datacenter.listProvisioningJobs])

  const handleCreateMissionFromProfile = async (profile: ProvisioningProfile) => {
    const mission = await missions.create.execute({
      name: `Provision — ${profile.name}`,
      goal: `Apply provisioning profile "${profile.name}" to target hosts. Configure OS, drivers, and software stack as defined.`,
      modelId: defaultModelId,
      target: 'all-hosts',
      trigger: 'manual',
      plan: { steps: [], estimatedDurationSeconds: 600 },
      permissions: { allowedPlugins: ['ssh', 'ipmi'], allowedTargets: ['all-hosts'], guardrails: ['backup-before-change', 'rollback-on-failure'] },
      tags: ['provisioning', profile.name],
    })
    notifications.show({ title: 'Mission created', message: mission.name, color: 'green' })
    navigate(`/missions/${mission.id}`)
  }

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} />

  return (
    <>
      <PageHeader title={t('provisioning.title')} subtitle="Manage images, profiles, and provisioning jobs" />

      <Tabs defaultValue="images">
        <Tabs.List mb="md">
          <Tabs.Tab value="images" leftSection={<IconPackage size={14} />}>{t('provisioning.images')} ({images.length})</Tabs.Tab>
          <Tabs.Tab value="profiles" leftSection={<IconSettings size={14} />}>{t('provisioning.profiles')} ({profiles.length})</Tabs.Tab>
          <Tabs.Tab value="jobs" leftSection={<IconHistory size={14} />}>{t('provisioning.jobs')} ({jobs.length})</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="images">
          {images.length === 0 ? (
            <EmptyState message={t('provisioning.empty.images')} />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>{t('common.version')}</Table.Th>
                  <Table.Th>OS</Table.Th>
                  <Table.Th>Arch</Table.Th>
                  <Table.Th>Size</Table.Th>
                  <Table.Th>{t('common.createdAt')}</Table.Th>
                  <Table.Th>Tags</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {images.map((img) => (
                  <Table.Tr key={img.id}>
                    <Table.Td><Text size="sm" fw={500}>{img.name}</Text></Table.Td>
                    <Table.Td><Badge size="xs" variant="outline">{img.version}</Badge></Table.Td>
                    <Table.Td><Text size="sm">{img.os}</Text></Table.Td>
                    <Table.Td><Text size="sm">{img.arch}</Text></Table.Td>
                    <Table.Td><Text size="sm">{img.size}</Text></Table.Td>
                    <Table.Td><Text size="sm" c="dimmed">{formatRelative(img.createdAt)}</Text></Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {img.tags?.map((tag) => <Badge key={tag} size="xs" variant="dot">{tag}</Badge>)}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="profiles">
          {profiles.length === 0 ? (
            <EmptyState message={t('provisioning.empty.profiles')} />
          ) : (
            <Stack gap="sm">
              {profiles.map((profile) => (
                <Card key={profile.id} withBorder>
                  <Group justify="space-between">
                    <Group gap="sm">
                      <ThemeIcon variant="light"><IconSettings size={16} /></ThemeIcon>
                      <Stack gap={0}>
                        <Text fw={500}>{profile.name}</Text>
                        <Text size="xs" c="dimmed">{profile.description}</Text>
                      </Stack>
                    </Group>
                    <Group gap="xs">
                      <Badge size="sm">{profile.imageName}</Badge>
                      <Button size="xs" variant="light" leftSection={<IconSparkles size={12} />}
                        onClick={() => void handleCreateMissionFromProfile(profile)}>
                        {t('provisioning.createMission')}
                      </Button>
                    </Group>
                  </Group>
                  {profile.packages && profile.packages.length > 0 && (
                    <Group gap={4} mt="xs">
                      {profile.packages.map((pkg) => <Badge key={pkg} size="xs" variant="outline">{pkg}</Badge>)}
                    </Group>
                  )}
                </Card>
              ))}
            </Stack>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="jobs">
          {jobs.length === 0 ? (
            <EmptyState message={t('provisioning.empty.jobs')} />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>ID</Table.Th>
                  <Table.Th>Profile</Table.Th>
                  <Table.Th>Target</Table.Th>
                  <Table.Th>{t('common.status')}</Table.Th>
                  <Table.Th>Started</Table.Th>
                  <Table.Th>Completed</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {jobs.map((job) => (
                  <Table.Tr key={job.id}>
                    <Table.Td><Text size="xs" ff="mono" c="dimmed">{job.id.slice(0, 8)}</Text></Table.Td>
                    <Table.Td><Text size="sm">{job.profileName}</Text></Table.Td>
                    <Table.Td><Text size="sm" ff="mono">{job.targetHostName}</Text></Table.Td>
                    <Table.Td>
                      <Badge size="sm" color={
                        job.status === 'succeeded' ? 'green' :
                        job.status === 'failed' ? 'red' :
                        job.status === 'running' ? 'blue' : 'gray'
                      }>{t(`provisioning.jobStatus.${job.status}`)}</Badge>
                    </Table.Td>
                    <Table.Td><Text size="sm" c="dimmed">{job.startedAt ? formatRelative(job.startedAt) : '—'}</Text></Table.Td>
                    <Table.Td><Text size="sm" c="dimmed">{job.completedAt ? formatRelative(job.completedAt) : '—'}</Text></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>
      </Tabs>
    </>
  )
}
