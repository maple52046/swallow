/**
 * The closed set of SSH Key purposes (glossary SSH Key, decision 039).
 *
 * - `deployment`: the single system-owned Deployment Key swallow itself logs in to Servers with;
 *   swallow holds its private key and never returns it.
 * - `access`: an Access Key a person logs in with; swallow stores only its public key.
 */
export type SSHKeyPurpose = 'deployment' | 'access'

/**
 * Realization state of one SSH Key in one provisioner Integration (glossary SSH Key).
 *
 * - `pending`: not yet realized since the key or the Integration changed.
 * - `synced`: the provisioner holds the key, so Servers it deploys authorize it.
 * - `failed`: the last attempt failed; `error` explains why.
 * - `unsupported`: the provisioner cannot hold SSH keys at all.
 */
export type SSHKeySyncState = 'pending' | 'synced' | 'failed' | 'unsupported'

/** One key's realization status in one provisioner Integration. */
export interface SSHKeyProviderSync {
  integrationId: string
  siteId: string
  state: SSHKeySyncState
  /** Last successful realization; absent when never synced. */
  syncedAt?: string
  /** Human-readable reason, present only when `state` is `failed`. */
  error?: string
}

/**
 * A swallow-owned SSH public key as the `ssh-keys.md` contract returns it. It never carries a
 * private key: the Deployment Key's private half stays sealed in swallow, and an Access Key's
 * private half is never stored at all. Public key material is not secret, so `publicKey` is
 * always present for copying into `authorized_keys`.
 */
export interface SSHKey {
  id: string
  name: string
  purpose: SSHKeyPurpose
  /** SSH algorithm name, e.g. `ssh-ed25519`. */
  keyType: string
  /** OpenSSH SHA256 fingerprint, unique across all keys. */
  fingerprint: string
  /** One authorized_keys line. */
  publicKey: string
  /** Owning User for an Access Key; absent for the system-owned Deployment Key. */
  ownerUserId?: string
  createdAt: string
  updatedAt: string
  /** One entry per enabled provisioner Integration; empty when the installation has none. */
  providerSync: SSHKeyProviderSync[]
}

/**
 * The one-time result of generating an Access Key pair. `privateKey` exists only in this response:
 * swallow keeps no copy, so the UI must let the operator save it before discarding it, and must
 * never persist or log it.
 */
export interface GeneratedAccessKey {
  key: SSHKey
  privateKey: string
}

/**
 * An API Key of the signed-in user as the `api-keys.md` contract returns it (glossary API Key,
 * decision 042): a long-lived secret that lets scripts and the CLI call Swallow as this user. The
 * secret itself is never part of it — swallow keeps only a hash — so `prefix` is what identifies a
 * key to a person.
 */
export interface ApiKey {
  id: string
  /** Unique among the user's keys. */
  name: string
  /** The first characters of the secret (`swk_…`); it cannot authenticate. */
  prefix: string
  createdAt: string
  /** `null` for a key that never expires. */
  expiresAt: string | null
  /** `null` until first use; updated at most once a minute, so it may lag slightly. */
  lastUsedAt: string | null
}

/**
 * The one-time result of creating an API Key. `secret` exists only in this response: the UI must
 * let the operator copy it before discarding it, and must never persist or log it.
 */
export interface CreatedApiKey {
  key: ApiKey
  secret: string
}

/** Whether the key has passed its expiry at `now` (a key without expiry never has). */
export function isApiKeyExpired(key: ApiKey, now: number): boolean {
  return key.expiresAt !== null && Date.parse(key.expiresAt) <= now
}
