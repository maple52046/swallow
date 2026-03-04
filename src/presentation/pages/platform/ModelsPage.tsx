import { useEffect, useState } from 'react'
import {
  Table, Badge, Group, Text, Button, ThemeIcon, ActionIcon,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconBrain, IconStar, IconStarFilled } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Model } from '@/domain/platform/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'

export function ModelsPage() {
  const { platform } = useApp()
  const [models, setModels] = useState<Model[]>([])
  const [loading, setLoading] = useState(true)

  const load = () => {
    platform.listModels.execute().then(setModels).catch(() => null).finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleSetDefault = async (model: Model) => {
    await platform.setDefaultModel.execute(model.id)
    notifications.show({ title: 'Default model set', message: model.name, color: 'green' })
    load()
  }

  if (loading) return <LoadingState />

  const defaultModel = models.find((m) => m.isDefault)

  return (
    <>
      <PageHeader
        title={t('platform.model.titlePlural')}
        subtitle={`${models.length} models · Default: ${defaultModel?.name ?? 'none'}`}
      />

      {models.length === 0 ? (
        <EmptyState message={t('platform.model.empty')} />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('common.name')}</Table.Th>
              <Table.Th>{t('platform.model.provider')}</Table.Th>
                  <Table.Th>Provider</Table.Th>
              <Table.Th>{t('platform.model.contextWindow')}</Table.Th>
              <Table.Th>{t('platform.model.cost')}</Table.Th>
              <Table.Th>{t('common.enabled')}</Table.Th>
              <Table.Th>{t('platform.model.default')}</Table.Th>
              <Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {models.map((model) => (
              <Table.Tr key={model.id}>
                <Table.Td>
                  <Group gap="xs">
                    <ThemeIcon size="sm" variant="light"><IconBrain size={12} /></ThemeIcon>
                    <Text size="sm" fw={500}>{model.name}</Text>
                  </Group>
                </Table.Td>
                <Table.Td><Badge size="sm" variant="outline">{model.provider}</Badge></Table.Td>
                    <Table.Td><Badge size="sm" variant="outline">{model.provider}</Badge></Table.Td>
                <Table.Td><Text size="sm">{model.contextWindow?.toLocaleString()}k</Text></Table.Td>
                <Table.Td><Text size="sm">${model.costPer1kTokens?.toFixed(4)}</Text></Table.Td>
                <Table.Td>
                  <Badge size="xs" color={model.enabled ? 'green' : 'gray'}>
                    {model.enabled ? t('common.enabled') : t('common.disabled')}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  {model.isDefault ? (
                    <IconStarFilled size={16} color="var(--mantine-color-yellow-5)" />
                  ) : (
                    <ActionIcon variant="subtle" size="sm" onClick={() => void handleSetDefault(model)}>
                      <IconStar size={16} />
                    </ActionIcon>
                  )}
                </Table.Td>
                <Table.Td>
                  <Button size="xs" variant="subtle" onClick={() => void handleSetDefault(model)} disabled={model.isDefault}>
                    {t('platform.model.setDefault')}
                  </Button>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
