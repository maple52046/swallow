import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { ModuleKind, ModuleSpecSchema } from '@/domain/module/types'

/** The read of one module's spec schema; `ready` with a null schema means it declares none. */
export type ModuleSpecSchemaState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; schema: ModuleSpecSchema | null }

const NO_SCHEMA: ModuleSpecSchemaState = { status: 'ready', schema: null }

/**
 * The spec schema module `<kind>/<id>` declares, from the build's module list. An undefined `id`
 * reads nothing and is ready with no schema, so a caller that has dedicated screens for a module
 * never waits on the list. `reload` retries a failed read.
 */
export function useModuleSpecSchema(kind: ModuleKind, id: string | undefined): {
  state: ModuleSpecSchemaState
  reload: () => void
} {
  const { modules } = useApp()
  const [result, setResult] = useState<{ key: string; state: ModuleSpecSchemaState } | null>(null)
  const [revision, setRevision] = useState(0)
  const key = id ? `${kind}/${id}#${revision}` : ''

  useEffect(() => {
    if (!id) return
    let cancelled = false
    modules
      .listModules()
      .then((list) => {
        const schema = list.find((module) => module.kind === kind && module.id === id)?.spec ?? null
        if (!cancelled) setResult({ key, state: { status: 'ready', schema } })
      })
      .catch((error: Error) => {
        if (!cancelled) setResult({ key, state: { status: 'error', message: error.message } })
      })
    return () => {
      cancelled = true
    }
  }, [id, key, kind, modules])

  const reload = useCallback(() => setRevision((current) => current + 1), [])
  if (!id) return { state: NO_SCHEMA, reload }
  return { state: result?.key === key ? result.state : { status: 'loading' }, reload }
}
