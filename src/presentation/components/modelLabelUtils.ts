import type { Model } from '@/domain/platform/types'

export function resolveModelInfo(modelId: string, modelsById: Map<string, Model>) {
  const model = modelsById.get(modelId)
  if (!model) {
    return {
      name: modelId,
      type: 'unknown' as const,
      searchText: modelId.toLowerCase(),
    }
  }
  return {
    name: model.name,
    type: model.type,
    searchText: `${model.name} ${model.type}`.toLowerCase(),
  }
}
