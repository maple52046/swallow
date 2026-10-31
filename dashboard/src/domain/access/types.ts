import type { Condition } from '@/domain/convergence/types'

/**
 * The closed set of SSH Key purposes (glossary SSH Key, decision 039).
 *
 * - `deployment`: the single system-owned Deployment Key swallow itself logs in to Servers with;
 *   swallow holds its private key and never returns it.
 * - `access`: an Access Key a person logs in with; swallow stores only its public key.
 */
export type SSHKeyPurpose = 'deployment' | 'access'

/**
 * One SSH Key's realization in one provisioner Integration (glossary SSH Key, decision 071), as
 * the backend's SSH Key controller reports it. `conditions` are empty until the controller first
 * judged the key there; `realized` is then `true` when the provisioner holds the key's current
 * material, or `false` with a reason: `capability_missing` (the provisioner cannot hold SSH keys),
 * `provider_unreachable`, or `failed` (the message says why). `observedAt` is null until then.
 */
export interface SSHKeyRealization {
  integrationId: string
  siteId: string
  observedGeneration: number
  observedAt: string | null
  conditions: Condition[]
}

/** What swallow registers for an SSH Key: its name, purpose, and public key material. */
export interface SSHKeySpec {
  name: string
  purpose: SSHKeyPurpose
  /** SSH algorithm name, e.g. `ssh-ed25519`. */
  keyType: string
  /** OpenSSH SHA256 fingerprint, unique across all keys. */
  fingerprint: string
  /** One authorized_keys line. */
  publicKey: string
}

/**
 * A swallow-owned SSH public key as the `ssh-keys.md` contract returns it: a migrated resource
 * whose spec is the key and whose status is its realization in every provisioner. It never carries
 * a private key: the Deployment Key's private half stays sealed in swallow, and an Access Key's
 * private half is never stored at all. Public key material is not secret, so `spec.publicKey` is
 * always present for copying into `authorized_keys`. `generation` increments only when the
 * Deployment Key is replaced.
 */
export interface SSHKey {
  id: string
  /** Owning User for an Access Key; absent for the system-owned Deployment Key. */
  ownerUserId?: string
  generation: number
  spec: SSHKeySpec
  status: {
    observedGeneration: number
    /** The summary over every provisioner; realized only when all hold the current generation. */
    conditions: Condition[]
    /** One entry per enabled provisioner Integration; empty when the installation has none. */
    provisioners: SSHKeyRealization[]
    createdAt: string
    updatedAt: string
  }
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
