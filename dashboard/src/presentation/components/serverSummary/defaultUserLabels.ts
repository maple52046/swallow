import type { ServerDefaultUserSource } from '@/domain/server/types'

/**
 * Operator wording for where a Server's effective default user comes from (glossary Server Default
 * User). Shared by the Connection card and the Default user dialog so both say it the same way; the
 * domain value stays `server` / `os_image`.
 */
export function defaultUserSourceLabel(source: ServerDefaultUserSource): string {
  return source === 'server' ? 'set on this Server' : 'from the OS image'
}
