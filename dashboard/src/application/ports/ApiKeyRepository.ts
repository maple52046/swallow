import type { ApiKey, CreatedApiKey } from '@/domain/access/types'

/**
 * Application port for the signed-in user's API Keys (decision 042, contract `api-keys.md`).
 *
 * Every call acts as the signed-in admin and sees only their own keys; deleting another user's key
 * is reported as not found. Implementations map the provider contract only and reject with the
 * shared error envelope: `validation_error` for a bad name or a past expiry, `conflict` for a
 * duplicate name or the per-user limit, `forbidden` when the request was not made with a password
 * Session.
 */
export interface ApiKeyRepository {
  /** The caller's keys, oldest first, including expired ones (so they can be deleted). */
  listApiKeys(): Promise<ApiKey[]>
  /**
   * Creates a key. `expiresAt` (ISO 8601, in the future) is omitted for a key that never expires.
   * The returned secret is shown exactly once; callers must hand it to the operator and drop it.
   */
  createApiKey(input: { name: string; expiresAt?: string }): Promise<CreatedApiKey>
  /** Deletes one of the caller's keys; it stops authenticating immediately. */
  deleteApiKey(id: string): Promise<void>
}
