/**
 * Helpers for the provider-neutral detail tables. Kept separate from `DetailViews` so that
 * file exports only components (Fast Refresh requires that).
 */
import type { DetailTable } from '@/domain/server/types'

/** Finds a table by title (case-insensitive), or undefined when the provider omitted it. */
export function findTable(tables: DetailTable[], title: string): DetailTable | undefined {
  return tables.find((table) => table.title.toLowerCase() === title.toLowerCase())
}
