import { Badge, Group, Text } from '@mantine/core'
import type { Model } from '@/domain/platform/types'
import { resolveModelInfo } from '@/presentation/components/modelLabelUtils'

export function ModelLabel({ modelId, modelsById }: { modelId: string; modelsById: Map<string, Model> }) {
  const info = resolveModelInfo(modelId, modelsById)

  return (
    <Group gap={6} wrap="nowrap">
      {info.type !== 'unknown' && (
        <Badge size="xs" variant="light" color={info.type === 'local' ? 'violet' : 'blue'}>
          {info.type}
        </Badge>
      )}
      <Text size="sm" span>
        {info.name}
      </Text>
    </Group>
  )
}
