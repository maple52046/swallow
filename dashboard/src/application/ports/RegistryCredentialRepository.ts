import type {
  CreateRegistryCredentialInput,
  RegistryCredential,
  ReplaceRegistryCredentialInput,
} from '@/domain/software/docker'

/**
 * Application port for Registry Credentials (decision 044): the installation-wide, sealed
 * credentials the Docker Host Explorer attaches to private image pulls.
 *
 * Implementations map the Active `registry-credentials.md` contract. Passwords are sent on create
 * and replace only and are never returned, so nothing here can read one back. Rejections carry the
 * API's HTTP `status` (400 invalid, 404 missing, 409 a credential for that registry exists) and its
 * human message. Every call requires an admin session.
 */
export interface RegistryCredentialRepository {
  /** Every credential ordered by registry; a small, bounded set. */
  list(): Promise<RegistryCredential[]>
  create(input: CreateRegistryCredentialInput): Promise<RegistryCredential>
  replace(id: string, input: ReplaceRegistryCredentialInput): Promise<RegistryCredential>
  remove(id: string): Promise<void>
}
