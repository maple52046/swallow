import { isApiKeyExpired, type ApiKey } from '@/domain/access/types'
import { formatDate, formatRelative } from '@/shared/utils/time'

/** Expiry choices offered when creating an API Key; `never` sends no expiresAt. */
export const API_KEY_EXPIRY_OPTIONS = [
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '365', label: '1 year' },
  { value: 'never', label: 'Never expires' },
] as const

export type ApiKeyExpiryChoice = (typeof API_KEY_EXPIRY_OPTIONS)[number]['value']

/** The default expiry: long enough for CI, short enough that a forgotten key does not live forever. */
export const DEFAULT_API_KEY_EXPIRY: ApiKeyExpiryChoice = '90'

/** Converts an expiry choice into the contract's absolute `expiresAt`, or undefined for never. */
export function expiresAtFor(choice: ApiKeyExpiryChoice, now: number): string | undefined {
  if (choice === 'never') return undefined
  return new Date(now + Number(choice) * 24 * 60 * 60 * 1000).toISOString()
}

/** Text for the Expires column; an expired key says so in words, never only by colour. */
export function apiKeyExpiryLabel(key: ApiKey, now: number): string {
  if (key.expiresAt === null) return 'Never'
  const date = formatDate(key.expiresAt)
  return isApiKeyExpired(key, now) ? `Expired ${date}` : date
}

/** Text for the Last used column. */
export function apiKeyLastUsedLabel(key: ApiKey): string {
  return key.lastUsedAt ? formatRelative(key.lastUsedAt) : 'Never used'
}
