import { useState, useCallback, useMemo } from 'react'
import {
  Alert, Anchor, Badge, Breadcrumbs, Button, Card, Checkbox,
  Code, CopyButton, Divider, Grid, Group, SegmentedControl,
  Select, Stack, Text, TextInput, Title, Tooltip, ActionIcon,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate, useParams } from 'react-router-dom'
import {
  IconAlertTriangle, IconCheck, IconCopy, IconPlus, IconTrash, IconWand,
} from '@tabler/icons-react'
import { useAuth } from '@/presentation/contexts/AuthContext'

// ─── Types ────────────────────────────────────────────────────────────────────

type RestartPolicy = 'no' | 'on-failure' | 'always' | 'unless-stopped'
type GpuMode = 'none' | 'nvidia' | 'rocm'
type Protocol = 'tcp' | 'udp'
type ExecutionMode = 'create' | 'run'

interface DeviceRow { host: string; container: string; permissions: string }
interface MountRow { host: string; container: string; readOnly: boolean }
interface PortRow { host: string; container: string; protocol: Protocol }
interface EnvRow { key: string; value: string }

interface ContainerFormState {
  name: string
  image: string
  restartPolicy: RestartPolicy
  privileged: boolean
  shmSize: string
  capAdd: string[]
  gpuMode: GpuMode
  devices: DeviceRow[]
  mounts: MountRow[]
  ports: PortRow[]
  envVars: EnvRow[]
  user: string
  executionMode: ExecutionMode
  detached: boolean
  interactive: boolean
  tty: boolean
  workingDir: string
  entrypoint: string
  cmd: string
}

type ProfileKey = 'generic' | 'nvidia' | 'rocm'

// ─── Profile defaults ─────────────────────────────────────────────────────────

const PROFILES: Record<ProfileKey, Partial<ContainerFormState>> = {
  generic: {
    gpuMode: 'none',
    privileged: false,
    shmSize: '',
    capAdd: [],
    devices: [],
    restartPolicy: 'no',
    executionMode: 'run',
    detached: true,
    interactive: true,
    tty: true,
    workingDir: '',
    entrypoint: '',
    cmd: '',
  },
  nvidia: {
    gpuMode: 'nvidia',
    privileged: false,
    shmSize: '1G',
    capAdd: [],
    devices: [],
    restartPolicy: 'no',
    executionMode: 'run',
    detached: true,
    interactive: true,
    tty: true,
    workingDir: '',
    entrypoint: '',
    cmd: '',
  },
  rocm: {
    gpuMode: 'rocm',
    privileged: true,
    shmSize: '1G',
    capAdd: ['SYS_PTRACE'],
    devices: [
      { host: '/dev/kfd', container: '/dev/kfd', permissions: 'rwm' },
      { host: '/dev/dri', container: '/dev/dri', permissions: 'rwm' },
    ],
    restartPolicy: 'no',
    executionMode: 'run',
    detached: true,
    interactive: true,
    tty: true,
    workingDir: '',
    entrypoint: '',
    cmd: '',
  },
}

const BLANK_FORM: ContainerFormState = {
  name: '',
  image: '',
  restartPolicy: 'no',
  privileged: false,
  shmSize: '',
  capAdd: [],
  gpuMode: 'none',
  devices: [],
  mounts: [],
  ports: [],
  envVars: [],
  user: '',
  executionMode: 'run',
  detached: true,
  interactive: true,
  tty: true,
  workingDir: '',
  entrypoint: '',
  cmd: '',
}

function applyProfile(base: ContainerFormState, key: ProfileKey): ContainerFormState {
  return { ...base, ...PROFILES[key] }
}

function isFormDirty(form: ContainerFormState): boolean {
  return form.name !== '' || form.image !== '' || form.mounts.length > 0
    || form.ports.length > 0 || form.envVars.length > 0 || form.user !== ''
    || form.cmd !== '' || form.entrypoint !== '' || form.workingDir !== ''
}

// ─── Payload builder ──────────────────────────────────────────────────────────

function shmSizeToBytes(s: string): number | undefined {
  if (!s) return undefined
  const m = s.match(/^(\d+)(G|M|K)?$/i)
  if (!m) return undefined
  const n = parseInt(m[1], 10)
  const unit = (m[2] ?? '').toUpperCase()
  if (unit === 'G') return n * 1073741824
  if (unit === 'M') return n * 1048576
  if (unit === 'K') return n * 1024
  return n
}

interface DockerPayload {
  name: string
  profile: ProfileKey | string
  executionMode: ExecutionMode
  Config: Record<string, unknown>
  HostConfig: Record<string, unknown>
}

function buildPayload(form: ContainerFormState, profile: ProfileKey): DockerPayload {
  const exposedPorts: Record<string, Record<string, never>> = {}
  const portBindings: Record<string, { HostPort: string }[]> = {}
  form.ports.forEach((p) => {
    if (p.host && p.container) {
      const key = `${p.container}/${p.protocol}`
      exposedPorts[key] = {}
      portBindings[key] = [{ HostPort: p.host }]
    }
  })

  const mounts = form.mounts
    .filter((m) => m.host && m.container)
    .map((m) => ({
      Type: 'bind',
      Source: m.host,
      Target: m.container,
      ReadOnly: m.readOnly,
    }))

  const devices = form.gpuMode !== 'nvidia'
    ? form.devices
        .filter((d) => d.host)
        .map((d) => ({
          PathOnHost: d.host,
          PathInContainer: d.container || d.host,
          CgroupPermissions: d.permissions || 'rwm',
        }))
    : []

  const deviceRequests = form.gpuMode === 'nvidia'
    ? [{ Driver: 'nvidia', Count: -1, Capabilities: [['gpu']] }]
    : undefined

  const env = form.envVars
    .filter((e) => e.key)
    .map((e) => `${e.key}=${e.value}`)

  const shm = shmSizeToBytes(form.shmSize)
  const entrypoint = form.entrypoint.trim().split(/\s+/).filter(Boolean)
  const cmd = form.cmd.trim().split(/\s+/).filter(Boolean)
  const isRun = form.executionMode === 'run'

  const payload: DockerPayload = {
    name: form.name,
    profile,
    executionMode: form.executionMode,
    Config: {
      Image: form.image,
      ...(env.length > 0 ? { Env: env } : {}),
      ...(Object.keys(exposedPorts).length > 0 ? { ExposedPorts: exposedPorts } : {}),
      ...(form.user ? { User: form.user } : {}),
      ...(isRun && form.tty ? { Tty: true } : {}),
      ...(isRun && form.interactive ? { OpenStdin: true } : {}),
      ...(form.workingDir ? { WorkingDir: form.workingDir } : {}),
      ...(entrypoint.length > 0 ? { Entrypoint: entrypoint } : {}),
      ...(cmd.length > 0 ? { Cmd: cmd } : {}),
    },
    HostConfig: {
      Privileged: form.privileged,
      RestartPolicy: { Name: form.restartPolicy },
      ...(shm !== undefined ? { ShmSize: shm } : {}),
      ...(form.capAdd.length > 0 ? { CapAdd: form.capAdd } : {}),
      ...(devices.length > 0 ? { Devices: devices } : {}),
      ...(mounts.length > 0 ? { Mounts: mounts } : {}),
      ...(Object.keys(portBindings).length > 0 ? { PortBindings: portBindings } : {}),
      ...(deviceRequests ? { DeviceRequests: deviceRequests } : {}),
    },
  }

  return payload
}

// ─── CLI preview builder ──────────────────────────────────────────────────────

function buildCliPreview(form: ContainerFormState): string {
  const isRun = form.executionMode === 'run'
  const parts: string[] = [`docker ${isRun ? 'run' : 'create'}`]

  // -dit flags (run mode only)
  if (isRun) {
    const flags = [
      form.detached ? 'd' : '',
      form.interactive ? 'i' : '',
      form.tty ? 't' : '',
    ].filter(Boolean).join('')
    if (flags) parts.push(`  -${flags}`)
  }

  if (form.name) parts.push(`  --name ${form.name}`)
  if (form.privileged) parts.push('  --privileged')
  if (form.shmSize) parts.push(`  --shm-size=${form.shmSize.toLowerCase()}`)
  form.capAdd.forEach((c) => parts.push(`  --cap-add=${c}`))
  if (form.gpuMode === 'nvidia') parts.push('  --gpus all')
  if (form.gpuMode === 'rocm') {
    form.devices.forEach((d) => parts.push(`  --device=${d.host}`))
  }
  form.mounts.filter((m) => m.host && m.container).forEach((m) => {
    const ro = m.readOnly ? ':ro' : ''
    parts.push(`  -v ${m.host}:${m.container}${ro}`)
  })
  form.ports.filter((p) => p.host && p.container).forEach((p) => {
    parts.push(`  -p ${p.host}:${p.container}/${p.protocol}`)
  })
  form.envVars.filter((e) => e.key).forEach((e) => {
    parts.push(`  -e ${e.key}=${e.value}`)
  })
  if (form.user) parts.push(`  --user ${form.user}`)
  if (form.restartPolicy !== 'no') parts.push(`  --restart ${form.restartPolicy}`)
  if (form.workingDir) parts.push(`  -w ${form.workingDir}`)

  // entrypoint — first token goes to --entrypoint (Docker CLI semantics)
  const ep = form.entrypoint.trim().split(/\s+/).filter(Boolean)
  if (ep.length > 0) parts.push(`  --entrypoint ${ep[0]}`)

  if (form.image) parts.push(`  ${form.image}`)

  // Entrypoint args (ep[1:]) prepended to cmd tail so they are not lost in preview
  const epArgs = ep.slice(1)
  const cmdTokens = form.cmd.trim().split(/\s+/).filter(Boolean)
  const tail = [...epArgs, ...cmdTokens]
  if (tail.length > 0) parts.push(`  ${tail.join(' ')}`)

  return parts.join(' \\\n')
}

// ─── Small helpers ────────────────────────────────────────────────────────────

function genSuffix() {
  return Math.random().toString(16).slice(2, 6)
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <Text fw={600} size="sm" tt="uppercase" c="dimmed" mt="sm">{children}</Text>
}

// ─── Page ─────────────────────────────────────────────────────────────────────

export function CreateContainerPage() {
  const { id: serverId } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { currentUser } = useAuth()

  const [profile, setProfile] = useState<ProfileKey>('generic')
  const [form, setForm] = useState<ContainerFormState>(() => applyProfile(BLANK_FORM, 'generic'))
  const [pendingProfile, setPendingProfile] = useState<ProfileKey | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [submitting, setSubmitting] = useState(false)

  // ── profile change ──
  const handleProfileChange = (key: ProfileKey) => {
    if (isFormDirty(form) && key !== profile) {
      setPendingProfile(key)
    } else {
      setProfile(key)
      setForm((f) => applyProfile(f, key))
    }
  }

  const confirmProfileChange = () => {
    if (!pendingProfile) return
    setProfile(pendingProfile)
    setForm((f) => applyProfile(f, pendingProfile))
    setPendingProfile(null)
  }

  // ── form setters ──
  const set = useCallback(<K extends keyof ContainerFormState>(key: K, value: ContainerFormState[K]) => {
    setForm((f) => ({ ...f, [key]: value }))
    setErrors((e) => { const c = { ...e }; delete c[key]; return c })
  }, [])

  // ── list row helpers ──
  const addDevice = () => set('devices', [...form.devices, { host: '', container: '', permissions: 'rwm' }])
  const removeDevice = (i: number) => set('devices', form.devices.filter((_, idx) => idx !== i))
  const setDevice = (i: number, patch: Partial<DeviceRow>) =>
    set('devices', form.devices.map((d, idx) => idx === i ? { ...d, ...patch } : d))

  const addMount = () => set('mounts', [...form.mounts, { host: '', container: '', readOnly: false }])
  const removeMount = (i: number) => set('mounts', form.mounts.filter((_, idx) => idx !== i))
  const setMount = (i: number, patch: Partial<MountRow>) =>
    set('mounts', form.mounts.map((m, idx) => idx === i ? { ...m, ...patch } : m))

  const addPort = () => set('ports', [...form.ports, { host: '', container: '', protocol: 'tcp' }])
  const removePort = (i: number) => set('ports', form.ports.filter((_, idx) => idx !== i))
  const setPort = (i: number, patch: Partial<PortRow>) =>
    set('ports', form.ports.map((p, idx) => idx === i ? { ...p, ...patch } : p))

  const addEnv = () => set('envVars', [...form.envVars, { key: '', value: '' }])
  const removeEnv = (i: number) => set('envVars', form.envVars.filter((_, idx) => idx !== i))
  const setEnv = (i: number, patch: Partial<EnvRow>) =>
    set('envVars', form.envVars.map((e, idx) => idx === i ? { ...e, ...patch } : e))

  const toggleCapAdd = (cap: string, checked: boolean) => {
    set('capAdd', checked ? [...form.capAdd, cap] : form.capAdd.filter((c) => c !== cap))
  }

  // ── entrypoint / cmd sleep infinity helper ──
  const useSleepInfinity = () => set('cmd', 'sleep infinity')

  // ── name auto-generate ──
  const autoName = () => {
    const user = currentUser?.username ?? 'user'
    set('name', `${user}-${genSuffix()}`)
  }

  // ── validation ──
  const validate = (): boolean => {
    const errs: Record<string, string> = {}
    if (!form.name.trim()) errs.name = 'Name is required'
    if (!form.image.trim()) errs.image = 'Image is required'
    if (form.shmSize && !/^\d+(G|M|K)?$/i.test(form.shmSize)) errs.shmSize = 'Invalid format (e.g. 1G, 512M)'
    form.ports.forEach((p, i) => {
      const hp = parseInt(p.host, 10)
      const cp = parseInt(p.container, 10)
      if (p.host && (isNaN(hp) || hp < 1 || hp > 65535)) errs[`port-host-${i}`] = 'Invalid port'
      if (p.container && (isNaN(cp) || cp < 1 || cp > 65535)) errs[`port-container-${i}`] = 'Invalid port'
    })
    setErrors(errs)
    return Object.keys(errs).length === 0
  }

  // ── warnings ──
  const warnings = useMemo(() => {
    const w: string[] = []
    if (form.gpuMode === 'rocm' && !form.devices.some((d) => d.host.includes('kfd')))
      w.push('ROCm profile: /dev/kfd device is missing')
    if (form.gpuMode === 'rocm' && !form.privileged)
      w.push('ROCm profile: privileged mode is recommended')
    return w
  }, [form])

  // ── payload / CLI (derived) ──
  const payload = useMemo(() => buildPayload(form, profile), [form, profile])
  const cliPreview = useMemo(() => buildCliPreview(form), [form])
  const payloadStr = useMemo(() => JSON.stringify(payload, null, 2), [payload])
  const epHasArgs = useMemo(
    () => form.entrypoint.trim().split(/\s+/).filter(Boolean).length > 1,
    [form.entrypoint]
  )

  // ── submit ──
  const handleSubmit = async () => {
    if (!validate()) return
    setSubmitting(true)
    await new Promise<void>((r) => setTimeout(r, 600))
    console.log('[CreateContainer] payload:', payload)
    setSubmitting(false)
    notifications.show({ title: 'Container created', message: `${form.name} created (mock)`, color: 'green' })
    navigate(`/servers/${serverId ?? ''}`)
  }

  const handleReset = () => {
    setForm((f) => applyProfile(f, profile))
    setErrors({})
  }

  // ── render ──
  return (
    <Stack gap="md">
      {/* Header */}
      <div>
        <Breadcrumbs mb="xs" fz="sm">
          <Anchor size="sm" onClick={() => navigate('/servers')}>Servers</Anchor>
          <Anchor size="sm" onClick={() => navigate(`/servers/${serverId ?? ''}`)}>Detail</Anchor>
          <Text size="sm">Create Container</Text>
        </Breadcrumbs>
        <Title order={3}>Create Container</Title>
        <Text size="sm" c="dimmed">Manually create a Docker container on this server</Text>
      </div>

      <Grid gutter="lg">
        {/* ── Left: Form ── */}
        <Grid.Col span={7}>
          <Stack gap="lg">

            {/* Profile Selector */}
            <Card withBorder>
              <Text fw={600} mb="sm">Profile</Text>
              <SegmentedControl
                fullWidth
                value={profile}
                onChange={(v) => handleProfileChange(v as ProfileKey)}
                data={[
                  { label: 'Generic', value: 'generic' },
                  { label: 'NVIDIA GPU', value: 'nvidia' },
                  { label: 'ROCm GPU', value: 'rocm' },
                ]}
              />
              {pendingProfile && (
                <Alert
                  color="yellow"
                  icon={<IconAlertTriangle size={16} />}
                  mt="sm"
                  title="Apply profile settings?"
                >
                  <Text size="sm">This will update GPU, privileged, devices and SHM size fields.</Text>
                  <Group gap="xs" mt="xs">
                    <Button size="xs" onClick={confirmProfileChange}>Apply</Button>
                    <Button size="xs" variant="default" onClick={() => setPendingProfile(null)}>Cancel</Button>
                  </Group>
                </Alert>
              )}
            </Card>

            {/* Validation warnings */}
            {warnings.length > 0 && (
              <Alert color="orange" icon={<IconAlertTriangle size={16} />} title="Configuration warnings">
                <Stack gap={4}>
                  {warnings.map((w, i) => <Text key={i} size="sm">{w}</Text>)}
                </Stack>
              </Alert>
            )}

            {/* Basic Settings */}
            <Card withBorder>
              <SectionTitle>Basic Settings</SectionTitle>
              <Stack gap="sm" mt="sm">
                <Group align="flex-end" gap="xs">
                  <TextInput
                    label="Name"
                    placeholder="my-container"
                    value={form.name}
                    onChange={(e) => set('name', e.target.value)}
                    error={errors.name}
                    required
                    style={{ flex: 1 }}
                  />
                  <Tooltip label="Generate name">
                    <ActionIcon variant="default" size="lg" mb={errors.name ? 20 : 0} onClick={autoName}>
                      <IconWand size={14} />
                    </ActionIcon>
                  </Tooltip>
                </Group>
                <TextInput
                  label="Image"
                  placeholder="ubuntu:22.04"
                  value={form.image}
                  onChange={(e) => set('image', e.target.value)}
                  error={errors.image}
                  required
                />
                <div>
                  <Text size="sm" fw={500} mb={4}>Execution Mode</Text>
                  <SegmentedControl
                    fullWidth
                    size="sm"
                    value={form.executionMode}
                    onChange={(v) => set('executionMode', v as ExecutionMode)}
                    data={[
                      { label: 'Create and Run', value: 'run' },
                      { label: 'Create only', value: 'create' },
                    ]}
                  />
                </div>
                <Select
                  label="Restart Policy"
                  value={form.restartPolicy}
                  onChange={(v) => set('restartPolicy', (v ?? 'no') as RestartPolicy)}
                  data={[
                    { label: 'No (default)', value: 'no' },
                    { label: 'On Failure', value: 'on-failure' },
                    { label: 'Always', value: 'always' },
                    { label: 'Unless Stopped', value: 'unless-stopped' },
                  ]}
                />
              </Stack>
            </Card>

            {/* Runtime */}
            <Card withBorder>
              <SectionTitle>Runtime</SectionTitle>
              <Stack gap="sm" mt="sm">
                <Checkbox
                  label="Privileged Mode"
                  description="Give extended privileges to this container"
                  checked={form.privileged}
                  onChange={(e) => set('privileged', e.currentTarget.checked)}
                />
                <TextInput
                  label="SHM Size"
                  placeholder="1G or 512M"
                  value={form.shmSize}
                  onChange={(e) => set('shmSize', e.target.value)}
                  error={errors.shmSize}
                />
                <TextInput
                  label="Working Directory"
                  placeholder="/opt"
                  value={form.workingDir}
                  onChange={(e) => set('workingDir', e.target.value)}
                />
                {form.executionMode === 'run' && (
                  <div>
                    <Text size="sm" fw={500} mb={6}>Run Options</Text>
                    <Group gap="lg">
                      <Checkbox
                        label="Detached (-d)"
                        checked={form.detached}
                        onChange={(e) => set('detached', e.currentTarget.checked)}
                      />
                      <Checkbox
                        label="Interactive (-i)"
                        checked={form.interactive}
                        onChange={(e) => set('interactive', e.currentTarget.checked)}
                      />
                      <Checkbox
                        label="TTY (-t)"
                        checked={form.tty}
                        onChange={(e) => set('tty', e.currentTarget.checked)}
                      />
                    </Group>
                  </div>
                )}
                <div>
                  <Text size="sm" fw={500} mb={4}>Capabilities (CapAdd)</Text>
                  <Group gap="xs">
                    {['SYS_PTRACE', 'NET_ADMIN', 'SYS_ADMIN', 'IPC_LOCK'].map((cap) => (
                      <Checkbox
                        key={cap}
                        label={cap}
                        size="xs"
                        checked={form.capAdd.includes(cap)}
                        onChange={(e) => toggleCapAdd(cap, e.currentTarget.checked)}
                      />
                    ))}
                  </Group>
                </div>
              </Stack>
            </Card>

            {/* Process */}
            <Card withBorder>
              <SectionTitle>Process</SectionTitle>
              <Stack gap="md" mt="sm">
                {/* Entrypoint */}
                <TextInput
                  label="Entrypoint"
                  placeholder="bash -lc"
                  description="Input will be split by spaces automatically."
                  value={form.entrypoint}
                  onChange={(e) => set('entrypoint', e.target.value)}
                  ff="mono"
                  size="sm"
                />

                <Divider />

                {/* Cmd / Args */}
                <div>
                  <Group justify="space-between" mb={4}>
                    <Text size="sm" fw={500}>Cmd / Args</Text>
                    <Button size="xs" variant="light" color="teal" onClick={useSleepInfinity}>
                      Use sleep infinity
                    </Button>
                  </Group>
                  <TextInput
                    placeholder="sleep infinity"
                    description="Input will be split by spaces automatically."
                    value={form.cmd}
                    onChange={(e) => set('cmd', e.target.value)}
                    ff="mono"
                    size="sm"
                  />
                </div>
              </Stack>
            </Card>

            {/* GPU & Devices */}
            <Card withBorder>
              <Group justify="space-between" mb="sm">
                <SectionTitle>GPU & Devices</SectionTitle>
                <Badge size="xs" variant="outline" color={form.gpuMode === 'none' ? 'gray' : 'green'}>
                  {form.gpuMode === 'none' ? 'No GPU' : form.gpuMode.toUpperCase()}
                </Badge>
              </Group>

              <SegmentedControl
                size="xs"
                value={form.gpuMode}
                onChange={(v) => set('gpuMode', v as GpuMode)}
                data={[
                  { label: 'None', value: 'none' },
                  { label: 'NVIDIA', value: 'nvidia' },
                  { label: 'ROCm', value: 'rocm' },
                ]}
                mb="sm"
              />

              {form.gpuMode === 'nvidia' && (
                <Alert color="blue" variant="light">
                  <Text size="xs">NVIDIA GPU: uses <Code>--gpus all</Code> / DeviceRequests in the payload.</Text>
                </Alert>
              )}

              {form.gpuMode !== 'nvidia' && (
                <Stack gap="xs">
                  <Group justify="space-between">
                    <Text size="sm" fw={500}>Devices</Text>
                    <Button size="xs" variant="subtle" leftSection={<IconPlus size={12} />} onClick={addDevice}>
                      Add Device
                    </Button>
                  </Group>
                  {form.devices.map((d, i) => (
                    <Grid key={i} gutter="xs" align="flex-end">
                      <Grid.Col span={5}>
                        <TextInput
                          label={i === 0 ? 'Host Path' : undefined}
                          placeholder="/dev/kfd"
                          value={d.host}
                          onChange={(e) => setDevice(i, { host: e.target.value })}
                          size="xs"
                        />
                      </Grid.Col>
                      <Grid.Col span={4}>
                        <TextInput
                          label={i === 0 ? 'Container Path' : undefined}
                          placeholder="same as host"
                          value={d.container}
                          onChange={(e) => setDevice(i, { container: e.target.value })}
                          size="xs"
                        />
                      </Grid.Col>
                      <Grid.Col span={2}>
                        <Select
                          label={i === 0 ? 'Perms' : undefined}
                          value={d.permissions}
                          onChange={(v) => setDevice(i, { permissions: v ?? 'rwm' })}
                          data={['rwm', 'rw', 'r']}
                          size="xs"
                        />
                      </Grid.Col>
                      <Grid.Col span={1}>
                        <ActionIcon color="red" variant="subtle" size="sm" onClick={() => removeDevice(i)}
                          mb={i === 0 ? 0 : 2}>
                          <IconTrash size={12} />
                        </ActionIcon>
                      </Grid.Col>
                    </Grid>
                  ))}
                  {form.devices.length === 0 && (
                    <Text size="xs" c="dimmed">No devices configured</Text>
                  )}
                </Stack>
              )}
            </Card>

            {/* Storage & Mounts */}
            <Card withBorder>
              <Group justify="space-between" mb="sm">
                <SectionTitle>Storage & Mounts</SectionTitle>
                <Button size="xs" variant="subtle" leftSection={<IconPlus size={12} />} onClick={addMount}>
                  Add Mount
                </Button>
              </Group>
              <Stack gap="xs">
                {/* Column headers */}
                {form.mounts.length > 0 && (
                  <Grid gutter="xs">
                    <Grid.Col span={5}><Text size="xs" fw={500} c="dimmed">Host Path</Text></Grid.Col>
                    <Grid.Col span={4}><Text size="xs" fw={500} c="dimmed">Container Path</Text></Grid.Col>
                    <Grid.Col span={2}><Text size="xs" fw={500} c="dimmed">Read-only</Text></Grid.Col>
                    <Grid.Col span={1} />
                  </Grid>
                )}
                {form.mounts.map((m, i) => (
                  <Grid key={i} gutter="xs" align="center">
                    <Grid.Col span={5}>
                      <TextInput
                        placeholder="/data/models"
                        value={m.host}
                        onChange={(e) => setMount(i, { host: e.target.value })}
                        size="xs"
                      />
                    </Grid.Col>
                    <Grid.Col span={4}>
                      <TextInput
                        placeholder="/models"
                        value={m.container}
                        onChange={(e) => setMount(i, { container: e.target.value })}
                        size="xs"
                      />
                    </Grid.Col>
                    <Grid.Col span={2}>
                      <Checkbox
                        size="xs"
                        checked={m.readOnly}
                        onChange={(e) => setMount(i, { readOnly: e.currentTarget.checked })}
                      />
                    </Grid.Col>
                    <Grid.Col span={1}>
                      <ActionIcon color="red" variant="subtle" size="sm" onClick={() => removeMount(i)}>
                        <IconTrash size={12} />
                      </ActionIcon>
                    </Grid.Col>
                  </Grid>
                ))}
                {form.mounts.length === 0 && <Text size="xs" c="dimmed">No mounts configured</Text>}
              </Stack>
            </Card>

            {/* Networking */}
            <Card withBorder>
              <Group justify="space-between" mb="sm">
                <SectionTitle>Networking</SectionTitle>
                <Button size="xs" variant="subtle" leftSection={<IconPlus size={12} />} onClick={addPort}>
                  Add Port
                </Button>
              </Group>
              <Stack gap="xs">
                {form.ports.map((p, i) => (
                  <Grid key={i} gutter="xs" align="flex-end">
                    <Grid.Col span={4}>
                      <TextInput
                        label={i === 0 ? 'Host Port' : undefined}
                        placeholder="8080"
                        value={p.host}
                        onChange={(e) => setPort(i, { host: e.target.value })}
                        error={errors[`port-host-${i}`]}
                        size="xs"
                      />
                    </Grid.Col>
                    <Grid.Col span={4}>
                      <TextInput
                        label={i === 0 ? 'Container Port' : undefined}
                        placeholder="8080"
                        value={p.container}
                        onChange={(e) => setPort(i, { container: e.target.value })}
                        error={errors[`port-container-${i}`]}
                        size="xs"
                      />
                    </Grid.Col>
                    <Grid.Col span={3}>
                      <Select
                        label={i === 0 ? 'Protocol' : undefined}
                        value={p.protocol}
                        onChange={(v) => setPort(i, { protocol: (v ?? 'tcp') as Protocol })}
                        data={['tcp', 'udp']}
                        size="xs"
                      />
                    </Grid.Col>
                    <Grid.Col span={1}>
                      <ActionIcon color="red" variant="subtle" size="sm" onClick={() => removePort(i)}>
                        <IconTrash size={12} />
                      </ActionIcon>
                    </Grid.Col>
                  </Grid>
                ))}
                {form.ports.length === 0 && <Text size="xs" c="dimmed">No port mappings configured</Text>}
              </Stack>
            </Card>

            {/* Environment & Access */}
            <Card withBorder>
              <Group justify="space-between" mb="sm">
                <SectionTitle>Environment & Access</SectionTitle>
                <Button size="xs" variant="subtle" leftSection={<IconPlus size={12} />} onClick={addEnv}>
                  Add Variable
                </Button>
              </Group>
              <Stack gap="xs">
                {form.envVars.map((e, i) => (
                  <Grid key={i} gutter="xs" align="flex-end">
                    <Grid.Col span={5}>
                      <TextInput
                        label={i === 0 ? 'Key' : undefined}
                        placeholder="HF_HOME"
                        value={e.key}
                        onChange={(ev) => setEnv(i, { key: ev.target.value })}
                        size="xs"
                        ff="mono"
                      />
                    </Grid.Col>
                    <Grid.Col span={6}>
                      <TextInput
                        label={i === 0 ? 'Value' : undefined}
                        placeholder="/models"
                        value={e.value}
                        onChange={(ev) => setEnv(i, { value: ev.target.value })}
                        size="xs"
                        ff="mono"
                      />
                    </Grid.Col>
                    <Grid.Col span={1}>
                      <ActionIcon color="red" variant="subtle" size="sm" onClick={() => removeEnv(i)}>
                        <IconTrash size={12} />
                      </ActionIcon>
                    </Grid.Col>
                  </Grid>
                ))}
                {form.envVars.length === 0 && <Text size="xs" c="dimmed">No environment variables</Text>}
              </Stack>

              <Divider my="sm" />
              <TextInput
                label="Run as User"
                placeholder="1000:1000"
                value={form.user}
                onChange={(e) => set('user', e.target.value)}
                size="xs"
              />
            </Card>

            {/* Actions */}
            <Group justify="space-between">
              <Button variant="default" onClick={() => navigate(`/servers/${serverId ?? ''}`)}>
                Cancel
              </Button>
              <Group gap="xs">
                <Button variant="subtle" onClick={handleReset}>
                  Reset to Profile Defaults
                </Button>
                <Button loading={submitting} onClick={() => void handleSubmit()}>
                  {form.executionMode === 'run' ? 'Create & Run' : 'Create Container'}
                </Button>
              </Group>
            </Group>
          </Stack>
        </Grid.Col>

        {/* ── Right: Preview Panel ── */}
        <Grid.Col span={5}>
          <div style={{ position: 'sticky', top: 16 }}>
            <Stack gap="md">
              {/* JSON Payload — shown first */}
              <Card withBorder>
                <Group justify="space-between" mb="sm">
                  <Text fw={600} size="sm">JSON Payload</Text>
                  <CopyButton value={payloadStr} timeout={1500}>
                    {({ copied, copy }) => (
                      <Tooltip label={copied ? 'Copied!' : 'Copy JSON'} withArrow>
                        <ActionIcon size="sm" variant="subtle" color={copied ? 'green' : 'gray'} onClick={copy}>
                          {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                        </ActionIcon>
                      </Tooltip>
                    )}
                  </CopyButton>
                </Group>
                <Code
                  block
                  style={{
                    fontSize: 11,
                    maxHeight: 320,
                    overflowY: 'auto',
                    whiteSpace: 'pre',
                    fontFamily: 'monospace',
                  }}
                >
                  {payloadStr}
                </Code>
              </Card>

              {/* Docker CLI — shown second */}
              <Card withBorder>
                <Group justify="space-between" mb="sm">
                  <Text fw={600} size="sm">Docker CLI</Text>
                  <CopyButton value={cliPreview} timeout={1500}>
                    {({ copied, copy }) => (
                      <Tooltip label={copied ? 'Copied!' : 'Copy CLI'} withArrow>
                        <ActionIcon size="sm" variant="subtle" color={copied ? 'green' : 'gray'} onClick={copy}>
                          {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                        </ActionIcon>
                      </Tooltip>
                    )}
                  </CopyButton>
                </Group>
                {epHasArgs && (
                  <Text size="xs" c="dimmed" mb="xs">
                    Entrypoint includes arguments. CLI preview is rendered for readability; JSON payload is the source of truth.
                  </Text>
                )}
                <Code
                  block
                  style={{
                    fontSize: 11,
                    maxHeight: 280,
                    overflowY: 'auto',
                    whiteSpace: 'pre',
                    fontFamily: 'monospace',
                  }}
                >
                  {cliPreview || '# fill in Name and Image to generate preview'}
                </Code>
              </Card>
            </Stack>
          </div>
        </Grid.Col>
      </Grid>
    </Stack>
  )
}
