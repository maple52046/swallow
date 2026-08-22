import { en } from './en'

type DeepValue<T> = T extends string
  ? string
  : T extends Record<string, unknown>
    ? { [K in keyof T]: DeepValue<T[K]> }
    : never

function getPath(obj: Record<string, unknown>, path: string): string | undefined {
  const keys = path.split('.')
  let current: unknown = obj
  for (const key of keys) {
    if (typeof current !== 'object' || current === null) return undefined
    current = (current as Record<string, unknown>)[key]
  }
  return typeof current === 'string' ? current : undefined
}

export function t(key: string, params?: Record<string, string | number>): string {
  const value = getPath(en as unknown as Record<string, unknown>, key) ?? key
  if (!params) return value
  return value.replace(/\{\{(\w+)\}\}/g, (_, k) => String(params[k] ?? `{{${k}}}`))
}

export type { DeepValue }
