/**
 * Docker Host Explorer types for the dashboard (root glossary: Docker Host Explorer; decision 043).
 *
 * These are live Docker Engine objects read through `api-server`'s `servers-docker.md` contract for
 * a Server where swallow installed Docker CE with `enableApi`. They are never stored by swallow and
 * carry no swallow identity: ids and names are the Engine's own. The contract JSON already matches
 * these shapes. This module holds no transport or React detail.
 */

/** The live Engine header shown above the explorer sections. */
export interface DockerEngineSummary {
  /** The Engine API address api-server dials (`tcp://host:2375`), for display only. */
  endpoint: string
  serverVersion: string
  apiVersion: string
  operatingSystem: string
  osType: string
  architecture: string
  kernelVersion: string
  storageDriver: string
  cpus: number
  memoryBytes: number
  containers: number
  containersRunning: number
  containersPaused: number
  containersStopped: number
  images: number
}

/** One image in the host's local store. `repoTags` never contains Docker's `<none>` placeholder. */
export interface DockerImage {
  id: string
  repoTags: string[]
  repoDigests: string[]
  sizeBytes: number
  /** ISO 8601. */
  createdAt: string
  /** True when no tag references the image any more. */
  dangling: boolean
}

/**
 * A completed pull: the canonical reference (untagged references resolve to `latest`), the registry
 * it resolved to, and whether a stored Registry Credential was sent.
 */
export interface DockerImagePullResult {
  reference: string
  status: string
  registry: string
  authenticated: boolean
}

/**
 * A swallow-owned, installation-wide credential for one image registry (glossary: Registry
 * Credential; decision 044; contract `registry-credentials.md`). The password is write-only: no
 * response carries it, so this type has no password field.
 */
export interface RegistryCredential {
  id: string
  /** Normalized registry host (optionally `:port`); `docker.io` for Docker Hub. */
  registry: string
  username: string
  createdAt: string
  updatedAt: string
  updatedBy: string
}

/** Intent to store a credential; the API normalizes `registry` and rejects a duplicate. */
export interface CreateRegistryCredentialInput {
  registry: string
  username: string
  password: string
}

/** Intent to replace a credential's username and password; its registry cannot change. */
export interface ReplaceRegistryCredentialInput {
  username: string
  password: string
}

/** Docker Hub's normalized registry name, the registry of every reference without a host. */
export const DOCKER_HUB_REGISTRY = 'docker.io'

/** The names Docker itself treats as Docker Hub in an image reference. */
const DOCKER_HUB_ALIASES = new Set(['docker.io', 'index.docker.io', 'registry-1.docker.io'])

/**
 * Further names operators type for Docker Hub when saving a credential. No image reference uses
 * them, so api-server folds them into `docker.io` only when it stores a credential.
 */
const DOCKER_HUB_CREDENTIAL_NAMES = new Set(['hub.docker.com', 'registry.hub.docker.com', 'hub.docker.io', 'index.docker.io/v1'])

/**
 * The registry key api-server will store for credential input, mirroring its normalization (trim,
 * lower-case, drop `http(s)://` and a trailing slash, fold every Docker Hub name into `docker.io`).
 * Returns `''` for blank input. It does not validate the host; api-server rejects an invalid one.
 */
export function normalizeRegistryInput(raw: string): string {
  const value = raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, '')
    .replace(/\/$/, '')
  return DOCKER_HUB_ALIASES.has(value) || DOCKER_HUB_CREDENTIAL_NAMES.has(value) ? DOCKER_HUB_REGISTRY : value
}

/**
 * The registry an image reference is pulled from, using the same rule as api-server (and Docker):
 * the part before the first `/` when it contains `.` or `:` or is `localhost`, otherwise Docker Hub.
 * The tag or digest is ignored. The dashboard uses it only to show which stored credential a pull
 * will use; api-server resolves it again authoritatively.
 */
export function imageReferenceRegistry(reference: string): string {
  const withoutDigest = reference.trim().split('@')[0]
  const lastSlash = withoutDigest.lastIndexOf('/')
  const lastColon = withoutDigest.lastIndexOf(':')
  const name = lastColon > lastSlash ? withoutDigest.slice(0, lastColon) : withoutDigest
  const slash = name.indexOf('/')
  if (slash === -1) return DOCKER_HUB_REGISTRY
  const first = name.slice(0, slash).toLowerCase()
  if (!/[.:]/.test(first) && first !== 'localhost') return DOCKER_HUB_REGISTRY
  return DOCKER_HUB_ALIASES.has(first) ? DOCKER_HUB_REGISTRY : first
}

/**
 * A container `state`, the Docker Engine's own value passed through unchanged (glossary lists
 * `created | restarting | running | removing | paused | exited | dead`). Typed as a string because a
 * newer Engine may report another value, which must be displayed as-is rather than as a failure.
 */
export type DockerContainerState = string

/** One container, running or not. */
export interface DockerContainer {
  id: string
  name: string
  image: string
  imageId: string
  command: string
  state: DockerContainerState
  /** The Engine's human-readable summary, for example "Up 2 hours". */
  status: string
  createdAt: string
  ports: DockerPort[]
  networks: string[]
  mounts: DockerMount[]
}

/** One exposed container port; `publicPort` is null when it is not published on the host. */
export interface DockerPort {
  ip: string
  privatePort: number
  publicPort: number | null
  protocol: string
}

/** One mount; `source` is the volume name for a volume mount and the host path for a bind mount. */
export interface DockerMount {
  type: string
  source: string
  destination: string
  readOnly: boolean
}

/** One volume; `createdAt` is null when the Engine does not report it. */
export interface DockerVolume {
  name: string
  driver: string
  mountpoint: string
  scope: string
  createdAt: string | null
  labels: Record<string, string>
}

/** One network; `predefined` marks the Engine's own bridge/host/none, which cannot be removed. */
export interface DockerNetwork {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  attachable: boolean
  predefined: boolean
  subnets: DockerSubnet[]
  createdAt: string | null
}

/** One IPAM pool; `gateway` is empty when none is configured. */
export interface DockerSubnet {
  subnet: string
  gateway: string
}

/** Intent to create (and by default start) a container; rules per `servers-docker.md`. */
export interface CreateDockerContainerInput {
  name?: string
  image: string
  command?: string[]
  env?: string[]
  ports?: Array<{ containerPort: number; hostPort?: number; protocol?: 'tcp' | 'udp' | 'sctp'; hostIp?: string }>
  volumes?: Array<{ source: string; target: string; readOnly?: boolean }>
  network?: string
  restartPolicy?: DockerRestartPolicy
  start?: boolean
}

/** The restart policies the contract accepts. */
export type DockerRestartPolicy = 'no' | 'always' | 'unless-stopped' | 'on-failure'

/** The create result; `started` is false when `start: false` was requested. */
export interface DockerContainerCreated {
  id: string
  warnings: string[]
  started: boolean
}

/** Intent to create a volume; the Engine generates a name when omitted, driver defaults to local. */
export interface CreateDockerVolumeInput {
  name?: string
  driver?: string
  labels?: Record<string, string>
}

/** Intent to create a network; `gateway` requires `subnet`, driver defaults to bridge. */
export interface CreateDockerNetworkInput {
  name: string
  driver?: string
  internal?: boolean
  attachable?: boolean
  subnet?: string
  gateway?: string
}

/** A container lifecycle verb; values match the contract's route segments. */
export type DockerContainerAction = 'start' | 'stop' | 'restart'

/** The Engine's naming rule for containers, volumes, and networks, mirrored for form feedback. */
export const DOCKER_OBJECT_NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/

/**
 * Splits a command line into an argument vector, honouring single and double quotes so
 * `nginx -g "daemon off;"` becomes `['nginx', '-g', 'daemon off;']`.
 *
 * It is a convenience for the create form, not a shell: there is no variable expansion, escaping
 * beyond quotes, or globbing, because the Engine receives the vector directly. An unterminated
 * quote keeps the rest of the line as one argument.
 */
export function splitCommandLine(text: string): string[] {
  const args: string[] = []
  let current = ''
  let quote: '"' | "'" | null = null
  let started = false
  for (const char of text) {
    if (quote) {
      if (char === quote) quote = null
      else current += char
      continue
    }
    if (char === '"' || char === "'") {
      quote = char
      started = true
      continue
    }
    if (/\s/.test(char)) {
      if (started) args.push(current)
      current = ''
      started = false
      continue
    }
    current += char
    started = true
  }
  if (started) args.push(current)
  return args
}

/** A parse outcome for one multi-line form field: the values, or the first offending line's reason. */
export type ParsedLines<T> = { ok: true; values: T[] } | { ok: false; error: string }

function nonEmptyLines(text: string): string[] {
  return text.split('\n').map((line) => line.trim()).filter((line) => line !== '')
}

/**
 * Parses one `KEY=value` environment entry per line. The key must be non-empty; the value may be
 * empty (`DEBUG=`). Mirrors the contract rule so the form can block submission early.
 */
export function parseEnvLines(text: string): ParsedLines<string> {
  const values: string[] = []
  for (const line of nonEmptyLines(text)) {
    const separator = line.indexOf('=')
    if (separator <= 0) return { ok: false, error: `"${line}" must be KEY=value.` }
    values.push(line)
  }
  return { ok: true, values }
}

type PortMapping = NonNullable<CreateDockerContainerInput['ports']>[number]

/**
 * Parses one published port per line in the familiar `docker run -p` forms:
 * `80`, `8080:80`, `127.0.0.1:8080:80`, each optionally suffixed `/tcp`, `/udp`, or `/sctp`.
 * A bare container port lets the Engine choose the host port. IPv6 host addresses are not
 * accepted here (the contract allows them; the form keeps the syntax unambiguous).
 */
export function parsePortLines(text: string): ParsedLines<PortMapping> {
  const values: PortMapping[] = []
  for (const line of nonEmptyLines(text)) {
    const [mapping, protocolPart, extra] = line.split('/')
    const protocol = (protocolPart ?? 'tcp').toLowerCase()
    if (extra !== undefined || (protocol !== 'tcp' && protocol !== 'udp' && protocol !== 'sctp')) {
      return { ok: false, error: `"${line}" has an unsupported protocol (use tcp, udp, or sctp).` }
    }
    const parts = mapping.split(':')
    if (parts.length > 3) return { ok: false, error: `"${line}" must be [hostIp:][hostPort:]containerPort.` }
    const numbers = parts.slice(parts.length === 3 ? 1 : 0).map((part) => (/^\d+$/.test(part) ? Number(part) : NaN))
    const containerPort = numbers[numbers.length - 1]
    const hostPort = numbers.length === 2 ? numbers[0] : 0
    if (!(containerPort >= 1 && containerPort <= 65535) || !(hostPort >= 0 && hostPort <= 65535)) {
      return { ok: false, error: `"${line}" needs ports between 1 and 65535.` }
    }
    values.push({
      containerPort,
      hostPort,
      protocol: protocol as PortMapping['protocol'],
      ...(parts.length === 3 ? { hostIp: parts[0] } : {}),
    })
  }
  return { ok: true, values }
}

type VolumeMapping = NonNullable<CreateDockerContainerInput['volumes']>[number]

/**
 * Parses one mount per line in the `docker run -v` form `source:/target[:ro]`, where `source` is a
 * volume name (created on demand) or an absolute host path (a bind mount).
 */
export function parseVolumeLines(text: string): ParsedLines<VolumeMapping> {
  const values: VolumeMapping[] = []
  for (const line of nonEmptyLines(text)) {
    const parts = line.split(':')
    const readOnly = parts.length === 3 && parts[2] === 'ro'
    if (parts.length < 2 || parts.length > 3 || (parts.length === 3 && !readOnly && parts[2] !== 'rw')) {
      return { ok: false, error: `"${line}" must be source:/target or source:/target:ro.` }
    }
    const [source, target] = parts
    if (!target.startsWith('/')) return { ok: false, error: `"${line}" needs an absolute container path.` }
    if (!source.startsWith('/') && !DOCKER_OBJECT_NAME_PATTERN.test(source)) {
      return { ok: false, error: `"${line}" needs a volume name or an absolute host path as its source.` }
    }
    values.push({ source, target, readOnly })
  }
  return { ok: true, values }
}

/**
 * Whether a Server's Docker CE Software Assignment means swallow installed Docker there, which is
 * when the Server detail shows its Containers tab.
 *
 * `installed` qualifies, and so does a re-apply in progress or one that failed (`pending`, `failed`,
 * `uninstalling`) as long as an earlier apply succeeded (`lastAppliedAt` set) — otherwise the tab
 * would vanish while the operator enables the Docker Engine API from it. A first install that has
 * not succeeded yet, and an `absent` record, do not qualify.
 */
export function hasSwallowInstalledDocker<T extends { state: string; lastAppliedAt: string | null }>(
  assignment: T | null,
): assignment is T {
  if (!assignment || assignment.state === 'absent') return false
  return assignment.state === 'installed' || assignment.lastAppliedAt !== null
}

/** A short image or container id for dense tables: the first 12 hex characters, without `sha256:`. */
export function shortDockerId(id: string): string {
  return id.replace(/^sha256:/, '').slice(0, 12)
}

/** Renders one port as Docker does, e.g. `0.0.0.0:8080->80/tcp` or `80/tcp` when unpublished. */
export function formatDockerPort(port: DockerPort): string {
  const target = `${port.privatePort}/${port.protocol}`
  if (port.publicPort === null) return target
  return `${port.ip ? `${port.ip}:` : ''}${port.publicPort}->${target}`
}
