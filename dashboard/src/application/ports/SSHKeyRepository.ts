import type { GeneratedAccessKey, SSHKey } from '@/domain/access/types'

/**
 * Application port for SSH key management (decision 039, contract `ssh-keys.md`).
 *
 * Every call acts as the signed-in admin: listed Access Keys are the caller's own, and deleting
 * another user's key is reported as not found. Implementations map the provider contract only and
 * must reject with the shared error envelope (`validation_error` for an unparseable key or bad
 * name, `conflict` for a duplicate key/name or an attempt to delete the Deployment Key).
 */
export interface SSHKeyRepository {
  /** The Deployment Key first (when it exists), then the caller's Access Keys oldest first. */
  listSSHKeys(): Promise<SSHKey[]>
  /**
   * Reads one key the caller may see (the Deployment Key or one of their Access Keys) with its
   * current provisioner sync status. Rejects with `not_found` for an unknown or another user's key.
   */
  getSSHKey(id: string): Promise<SSHKey>
  /** Stores an existing public key as one of the caller's Access Keys. */
  importAccessKey(input: { name: string; publicKey: string }): Promise<SSHKey>
  /**
   * Generates an ed25519 Access Key pair. The returned private key is shown exactly once; callers
   * must hand it to the operator and then drop it.
   */
  generateAccessKey(name: string): Promise<GeneratedAccessKey>
  /** Deletes one of the caller's Access Keys and removes it from provisioners swallow registered it in. */
  deleteAccessKey(id: string): Promise<void>
  /**
   * Replaces the Deployment Key with an existing unencrypted private key. The private key is sent
   * once and never returned; `name` defaults to the current name.
   */
  replaceDeploymentKey(input: { privateKey: string; name?: string }): Promise<SSHKey>
  /** Replaces the Deployment Key with a freshly generated ed25519 key pair. */
  regenerateDeploymentKey(): Promise<SSHKey>
  /** Requests an immediate provisioner sync; resolves once accepted, before the sync runs. */
  requestSync(): Promise<void>
}
