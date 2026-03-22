import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ActionIcon, Anchor, Badge, Breadcrumbs, Button, Card, Checkbox,
  CopyButton, Divider, Grid, Group, NumberInput, PasswordInput,
  ScrollArea, Stack, Switch, Table, Tabs, Text, TextInput,
  ThemeIcon, Timeline, Title, Tooltip,
} from '@mantine/core'
import { AreaChart } from '@mantine/charts'
import { notifications } from '@mantine/notifications'
import { useNavigate, useParams } from 'react-router-dom'
import {
  IconAdjustments, IconAlertTriangle, IconCheck, IconClock,
  IconCopy, IconCpu, IconDatabase, IconDeviceDesktopAnalytics,
  IconEdit, IconEye, IconEyeOff, IconNetwork, IconPlayerPlay,
  IconPlus, IconPower, IconRefresh, IconRotate, IconServer,
  IconServerOff, IconTerminal2, IconTool, IconUser, IconUsers, IconX,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { useAuth } from '@/presentation/contexts/AuthContext'
import type { Server } from '@/domain/server/types'
import type { Alert } from '@/domain/alert/types'
import type { Team } from '@/domain/team/types'
import { getAllocationState } from '@/domain/server/types'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { formatRelative, formatDateTime } from '@/shared/utils/time'

// ─── Types ───────────────────────────────────────────────────────────────────

type TrendPoint = { h: string; v: number }

interface ContainerWorkload {
  id: string
  name: string
  image: string
  state: 'running' | 'stopped' | 'exited' | 'restarting'
  restartCount: number
  cpuPct: number
  ramPct: number
  startedAt: string
}

interface OpsEvent {
  title: string
  actor: string
  time: string
  icon: React.ReactNode
}

interface SystemSetting {
  key: string
  name: string
  description: string
  category: string
  runtimeValue: boolean
  persistentValue: boolean
  mutable: boolean
  requiresReboot: boolean
}

// ─── Mock data generators ────────────────────────────────────────────────────

function hashId(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) & 0xffffff
  }
  return h
}

function genTrend(seed: number, base: number, amplitude: number): TrendPoint[] {
  return Array.from({ length: 72 }, (_, i) => {
    const noise = Math.sin(seed * 1.3 + i * 0.7) * 3
    const v = Math.round(Math.max(0, Math.min(100,
      base + Math.sin((i + seed) * 0.3) * amplitude + noise
    )))
    return { h: `${71 - i}h`, v }
  }).reverse()
}

function genWorkloads(server: Server): ContainerWorkload[] {
  const h = hashId(server.id)
  if (server.status === 'offline' || server.status === 'maintain') return []
  const count = 2 + (h % 3)
  const daysAgo = (d: number) => new Date(Date.now() - d * 86400 * 1000).toISOString()
  const images = [
    'pytorch/pytorch:2.3.0-cuda12.1-cudnn8-runtime',
    'nvcr.io/nvidia/tritonserver:24.05-py3',
    'vllm/vllm-openai:v0.4.3',
    'ghcr.io/huggingface/text-generation-inference:2.0',
    'ollama/ollama:0.1.44',
    'alpine:3.19',
  ]
  const names = [
    'training-job', 'inference-server', 'vllm-api', 'tgi-service',
    'ollama-runner', 'data-pipeline', 'monitor-agent', 'checkpoint-saver',
  ]
  const states: ContainerWorkload['state'][] = ['running', 'running', 'running', 'stopped', 'exited']
  return Array.from({ length: count }, (_, i) => ({
    id: `${server.id}-wl-${i}`,
    name: names[(h + i) % names.length],
    image: images[(h + i * 3) % images.length],
    state: states[(h + i) % states.length],
    restartCount: (h + i) % 4,
    cpuPct: ((h * (i + 1)) % 80) + 5,
    ramPct: ((h * (i + 2)) % 70) + 10,
    startedAt: daysAgo(((h + i) % 7) + 1),
  }))
}

function genTimeline(server: Server): OpsEvent[] {
  const h = hashId(server.id)
  const minsAgo = (m: number) => new Date(Date.now() - m * 60 * 1000).toISOString()
  const hoursAgo = (hrs: number) => new Date(Date.now() - hrs * 3600 * 1000).toISOString()
  const daysAgo = (d: number) => new Date(Date.now() - d * 86400 * 1000).toISOString()

  const actors = ['system', 'admin', 'owner', 'automation']
  const baseEvents: OpsEvent[] = [
    { title: 'Agent heartbeat received', actor: 'system', time: minsAgo(2 + (h % 5)), icon: <IconCheck size={14} /> },
    { title: 'Metrics collected', actor: 'system', time: minsAgo(10 + (h % 10)), icon: <IconDeviceDesktopAnalytics size={14} /> },
    { title: 'Assigned to team', actor: actors[(h + 1) % actors.length], time: hoursAgo(2 + (h % 6)), icon: <IconUsers size={14} /> },
    { title: 'Status changed to live', actor: 'system', time: hoursAgo(8 + (h % 12)), icon: <IconServer size={14} /> },
    { title: 'Server powered on', actor: actors[(h + 2) % actors.length], time: daysAgo(1 + (h % 3)), icon: <IconPower size={14} /> },
    { title: 'Hardware inventory synced', actor: 'system', time: daysAgo(3 + (h % 5)), icon: <IconDatabase size={14} /> },
    { title: 'SSH credentials updated', actor: actors[(h + 3) % actors.length], time: daysAgo(7 + (h % 7)), icon: <IconTerminal2 size={14} /> },
    { title: 'Server registered', actor: 'admin', time: server.createdAt, icon: <IconPlayerPlay size={14} /> },
  ]

  return baseEvents.slice(0, 5 + (h % 4))
}

// ─── Sub-components ───────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: Server['status'] }) {
  const map: Record<Server['status'], { color: string; label: string }> = {
    live: { color: 'green', label: 'Live' },
    warning: { color: 'yellow', label: 'Warning' },
    error: { color: 'red', label: 'Error' },
    maintain: { color: 'blue', label: 'Maintenance' },
    offline: { color: 'gray', label: 'Offline' },
    unknown: { color: 'gray', label: 'Unknown' },
  }
  const { color, label } = map[status]
  return <Badge color={color} variant="light" size="sm">{label}</Badge>
}

function AlertSeverityBadge({ severity }: { severity: Alert['severity'] }) {
  const map: Record<Alert['severity'], { color: string }> = {
    critical: { color: 'red' },
    warning: { color: 'yellow' },
    info: { color: 'blue' },
  }
  return <Badge color={map[severity].color} size="xs" variant="filled">{severity}</Badge>
}

function WorkloadStateBadge({ state }: { state: ContainerWorkload['state'] }) {
  const map: Record<ContainerWorkload['state'], { color: string; label: string }> = {
    running: { color: 'green', label: 'Running' },
    stopped: { color: 'gray', label: 'Stopped' },
    exited: { color: 'red', label: 'Exited' },
    restarting: { color: 'orange', label: 'Restarting' },
  }
  const { color, label } = map[state]
  return <Badge color={color} size="xs" variant="light">{label}</Badge>
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <Group justify="space-between" py={4} style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
      <Text size="sm" c="dimmed" w={120}>{label}</Text>
      <Text size="sm" style={{ flex: 1, textAlign: 'right' }}>{value}</Text>
    </Group>
  )
}

function MetricCard({ label, value, color, icon }: {
  label: string; value: string; color: string; icon: React.ReactNode
}) {
  return (
    <Card withBorder radius="md" style={{ borderLeft: `3px solid var(--mantine-color-${color}-5)`, flex: 1 }}>
      <Stack gap={4}>
        <Group gap="xs">
          <ThemeIcon variant="light" color={color} size="sm">{icon}</ThemeIcon>
          <Text size="xs" c="dimmed" fw={500}>{label}</Text>
        </Group>
        <Text fz={28} fw={800} lh={1}>{value}</Text>
      </Stack>
    </Card>
  )
}

function TrendPanel({ label, color, data, icon }: {
  label: string; color: string; data: TrendPoint[]; icon: React.ReactNode
}) {
  return (
    <Card withBorder radius="md">
      <Group gap="xs" mb="xs">
        <ThemeIcon variant="light" color={color} size="sm">{icon}</ThemeIcon>
        <Text size="sm" fw={500} c="dimmed">{label} — 72h trend</Text>
      </Group>
      <AreaChart
        h={100}
        data={data}
        dataKey="h"
        series={[{ name: 'v', color }]}
        withDots={false}
        withXAxis={false}
        withYAxis={false}
        gridAxis="none"
        fillOpacity={0.15}
        curveType="natural"
      />
    </Card>
  )
}

// ─── SshCard (view + inline edit) ────────────────────────────────────────────

function SshCard({
  server,
  onUpdate,
}: {
  server: Server
  onUpdate: (patch: Partial<Server>) => Promise<void>
}) {
  const { currentUser } = useAuth()
  const [isEditing, setIsEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [showSshPw, setShowSshPw] = useState(false)

  // edit fields
  const [editHost, setEditHost] = useState('')
  const [editPort, setEditPort] = useState<number | string>(22)
  const [editKeyAuth, setEditKeyAuth] = useState(false)
  const [editPwAuth, setEditPwAuth] = useState(false)
  const [editLoginUser, setEditLoginUser] = useState('')
  const [editPassword, setEditPassword] = useState('')
  const [loginUserError, setLoginUserError] = useState<string | null>(null)
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const editHostManuallyChanged = useRef(false)

  const enterEdit = () => {
    editHostManuallyChanged.current = false
    setEditHost(server.ssh?.host ?? server.ip)
    setEditPort(server.ssh?.port ?? 22)
    setEditKeyAuth(server.ssh?.keyAuthEnabled ?? false)
    setEditPwAuth(server.ssh?.passwordAuthEnabled ?? false)
    setEditLoginUser(server.ssh?.username ?? '')
    setEditPassword(server.ssh?.password ?? '')
    setLoginUserError(null)
    setPasswordError(null)
    setIsEditing(true)
  }

  const cancelEdit = () => {
    setIsEditing(false)
  }

  const handleSave = async () => {
    let valid = true
    if (editPwAuth) {
      if (!editLoginUser.trim()) { setLoginUserError('Login user is required'); valid = false } else setLoginUserError(null)
      if (!editPassword.trim()) { setPasswordError('Password is required'); valid = false } else setPasswordError(null)
    } else {
      setLoginUserError(null)
      setPasswordError(null)
    }
    if (!valid) return

    try {
      setSaving(true)
      await onUpdate({
        ssh: {
          host: editHost.trim() || server.ip,
          port: typeof editPort === 'number' ? editPort : 22,
          username: editLoginUser.trim(),
          keyAuthEnabled: editKeyAuth,
          passwordAuthEnabled: editPwAuth,
          password: editPwAuth ? editPassword.trim() : undefined,
        },
      })
      setIsEditing(false)
    } finally {
      setSaving(false)
    }
  }

  // derive display values
  const sshUser = server.ssh?.passwordAuthEnabled
    ? (server.ssh.username || '?')
    : (currentUser?.username ?? '?')

  const sshCmd = server.ssh
    ? (server.ssh.port !== 22
        ? `ssh ${sshUser}@${server.ssh.host} -p ${server.ssh.port}`
        : `ssh ${sshUser}@${server.ssh.host}`)
    : null

  return (
    <Card withBorder>
      <Group justify="space-between" mb="sm">
        <Group gap="xs">
          <IconTerminal2 size={14} />
          <Text size="sm" fw={600}>SSH</Text>
        </Group>
        {!isEditing && (
          <ActionIcon size="xs" variant="subtle" color="gray" onClick={enterEdit}>
            <IconEdit size={12} />
          </ActionIcon>
        )}
      </Group>

      {/* ── View Mode ── */}
      {!isEditing && (
        <Stack gap={0}>
          {!server.ssh ? (
            <>
              <InfoRow label="SSH" value="N/A" />
              <Button size="xs" variant="subtle" mt="xs" onClick={enterEdit}>Configure</Button>
            </>
          ) : (
            <>
              <InfoRow label="Host" value={<Text size="sm" ff="mono">{server.ssh.host}</Text>} />
              <InfoRow label="Port" value={<Text size="sm" ff="mono">{server.ssh.port}</Text>} />
              <Group justify="space-between" py={4} style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
                <Text size="sm" c="dimmed" w={120}>Connect</Text>
                {sshCmd ? (
                  <Group gap="xs" style={{ flex: 1, justifyContent: 'flex-end' }}>
                    <Text size="sm" ff="mono" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {sshCmd}
                    </Text>
                    <CopyButton value={sshCmd} timeout={1500}>
                      {({ copied, copy }) => (
                        <Tooltip label={copied ? 'Copied!' : 'Copy'} withArrow position="top">
                          <ActionIcon size="xs" variant="subtle" color={copied ? 'green' : 'gray'} onClick={copy}>
                            {copied ? <IconCheck size={12} /> : <IconCopy size={12} />}
                          </ActionIcon>
                        </Tooltip>
                      )}
                    </CopyButton>
                  </Group>
                ) : (
                  <Text size="sm" c="dimmed">N/A</Text>
                )}
              </Group>
              {server.ssh.passwordAuthEnabled && (
                <Group justify="space-between" py={4} style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
                  <Text size="sm" c="dimmed" w={120}>Password</Text>
                  <Group gap="xs" style={{ flex: 1, justifyContent: 'flex-end' }}>
                    <Text size="sm" ff="mono">
                      {server.ssh.password
                        ? (showSshPw ? server.ssh.password : '••••••••')
                        : 'N/A'}
                    </Text>
                    {server.ssh.password && (
                      <ActionIcon size="xs" variant="subtle" color="gray" onClick={() => setShowSshPw((v) => !v)}>
                        {showSshPw ? <IconEyeOff size={12} /> : <IconEye size={12} />}
                      </ActionIcon>
                    )}
                  </Group>
                </Group>
              )}
              <InfoRow
                label="Authentications"
                value={
                  <Group gap="xs" justify="flex-end">
                    <Badge size="xs" variant="filled" color={server.ssh.keyAuthEnabled ? 'green' : 'gray'}>
                      Key
                    </Badge>
                    <Badge size="xs" variant="filled" color={server.ssh.passwordAuthEnabled ? 'green' : 'gray'}>
                      Password
                    </Badge>
                  </Group>
                }
              />
            </>
          )}
        </Stack>
      )}

      {/* ── Edit Mode ── */}
      {isEditing && (
        <Stack gap="sm">
          <Divider label="Edit SSH Configuration" labelPosition="left" />
          <TextInput
            label="SSH Host / IP"
            value={editHost}
            onChange={(e) => { editHostManuallyChanged.current = true; setEditHost(e.target.value) }}
            placeholder={server.ip}
            size="xs"
          />
          <NumberInput
            label="Port"
            value={editPort}
            onChange={setEditPort}
            min={1}
            max={65535}
            size="xs"
          />
          <Checkbox
            label="Enable key authentication"
            checked={editKeyAuth}
            onChange={(e) => setEditKeyAuth(e.currentTarget.checked)}
            size="sm"
          />
          <Checkbox
            label="Enable password authentication"
            checked={editPwAuth}
            onChange={(e) => setEditPwAuth(e.currentTarget.checked)}
            size="sm"
          />
          {editPwAuth && (
            <Stack gap="sm" pl="md">
              <TextInput
                label="Login User"
                value={editLoginUser}
                onChange={(e) => { setEditLoginUser(e.target.value); setLoginUserError(null) }}
                error={loginUserError}
                size="xs"
                required
              />
              <PasswordInput
                label="Password"
                value={editPassword}
                onChange={(e) => { setEditPassword(e.target.value); setPasswordError(null) }}
                error={passwordError}
                size="xs"
                required
              />
            </Stack>
          )}
          <Group gap="xs" justify="flex-end">
            <Button size="xs" variant="default" leftSection={<IconX size={12} />} onClick={cancelEdit} disabled={saving}>
              Cancel
            </Button>
            <Button size="xs" loading={saving} onClick={() => void handleSave()}>
              Save
            </Button>
          </Group>
        </Stack>
      )}
    </Card>
  )
}

// ─── BmcCard (view + inline edit) ─────────────────────────────────────────────

function BmcCard({
  server,
  onUpdate,
}: {
  server: Server
  onUpdate: (patch: Partial<Server>) => Promise<void>
}) {
  const [isEditing, setIsEditing] = useState(false)
  const [saving, setSaving] = useState(false)

  // view: password visibility
  const [showIpmiPw, setShowIpmiPw] = useState(false)
  const [showRedfishPw, setShowRedfishPw] = useState(false)

  // edit fields
  const [ipmiEnabled, setIpmiEnabled] = useState(false)
  const [ipmiHost, setIpmiHost] = useState('')
  const [ipmiPort, setIpmiPort] = useState<number | string>(623)
  const [ipmiUsername, setIpmiUsername] = useState('')
  const [ipmiPassword, setIpmiPassword] = useState('')

  const [redfishEnabled, setRedfishEnabled] = useState(false)
  const [redfishHost, setRedfishHost] = useState('')
  const [redfishPort, setRedfishPort] = useState<number | string>(443)
  const [redfishUsername, setRedfishUsername] = useState('')
  const [redfishPassword, setRedfishPassword] = useState('')

  const [ipmiHostError, setIpmiHostError] = useState<string | null>(null)
  const [redfishHostError, setRedfishHostError] = useState<string | null>(null)

  const enterEdit = () => {
    setIpmiEnabled(!!server.bmc?.ipmi)
    setIpmiHost(server.bmc?.ipmi?.host ?? '')
    setIpmiPort(server.bmc?.ipmi?.port ?? 623)
    setIpmiUsername(server.bmc?.ipmi?.username ?? '')
    setIpmiPassword(server.bmc?.ipmi?.password ?? '')
    setRedfishEnabled(!!server.bmc?.redfish)
    setRedfishHost(server.bmc?.redfish?.host ?? '')
    setRedfishPort(server.bmc?.redfish?.port ?? 443)
    setRedfishUsername(server.bmc?.redfish?.username ?? '')
    setRedfishPassword(server.bmc?.redfish?.password ?? '')
    setIpmiHostError(null)
    setRedfishHostError(null)
    setIsEditing(true)
  }

  const cancelEdit = () => setIsEditing(false)

  const handleSave = async () => {
    let valid = true
    if (ipmiEnabled && !ipmiHost.trim()) { setIpmiHostError('IPMI host is required'); valid = false } else setIpmiHostError(null)
    if (redfishEnabled && !redfishHost.trim()) { setRedfishHostError('Redfish host is required'); valid = false } else setRedfishHostError(null)
    if (!valid) return

    try {
      setSaving(true)
      const newBmc = (ipmiEnabled || redfishEnabled) ? {
        address: ipmiEnabled ? ipmiHost.trim() : redfishHost.trim(),
        vendor: server.bmc?.vendor,
        ipmi: ipmiEnabled ? {
          host: ipmiHost.trim(),
          port: typeof ipmiPort === 'number' ? ipmiPort : undefined,
          username: ipmiUsername.trim() || undefined,
          password: ipmiPassword.trim() || undefined,
        } : undefined,
        redfish: redfishEnabled ? {
          host: redfishHost.trim(),
          port: typeof redfishPort === 'number' ? redfishPort : undefined,
          username: redfishUsername.trim() || undefined,
          password: redfishPassword.trim() || undefined,
        } : undefined,
      } : undefined
      await onUpdate({ bmc: newBmc })
      setIsEditing(false)
    } finally {
      setSaving(false)
    }
  }

  const hasIpmi = !!server.bmc?.ipmi
  const hasRedfish = !!server.bmc?.redfish

  return (
    <Card withBorder>
      <Group justify="space-between" mb="sm">
        <Group gap="xs">
          <IconServer size={14} />
          <Text size="sm" fw={600}>BMC</Text>
        </Group>
        {!isEditing && (
          <ActionIcon size="xs" variant="subtle" color="gray" onClick={enterEdit}>
            <IconEdit size={12} />
          </ActionIcon>
        )}
      </Group>

      {/* ── View Mode ── */}
      {!isEditing && (
        <>
          {!hasIpmi && !hasRedfish ? (
            <>
              <InfoRow label="BMC" value="N/A" />
              <Button size="xs" variant="subtle" mt="xs" onClick={enterEdit}>Configure</Button>
            </>
          ) : (
            <Stack gap="md">
              {hasIpmi && server.bmc?.ipmi && (
                <div>
                  <Text size="xs" c="dimmed" fw={600} tt="uppercase" mb={4}>IPMI</Text>
                  <Stack gap={0}>
                    <InfoRow label="Host" value={<Text size="sm" ff="mono">{server.bmc.ipmi.host}</Text>} />
                    <InfoRow label="Port" value={server.bmc.ipmi.port ? <Text size="sm" ff="mono">{server.bmc.ipmi.port}</Text> : '—'} />
                    <InfoRow label="Username" value={server.bmc.ipmi.username ?? '—'} />
                    <Group justify="space-between" py={4} style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
                      <Text size="sm" c="dimmed" w={120}>Password</Text>
                      <Group gap="xs" style={{ flex: 1, justifyContent: 'flex-end' }}>
                        <Text size="sm" ff="mono">
                          {server.bmc.ipmi.password ? (showIpmiPw ? server.bmc.ipmi.password : '••••••••') : '—'}
                        </Text>
                        {server.bmc.ipmi.password && (
                          <ActionIcon size="xs" variant="subtle" color="gray" onClick={() => setShowIpmiPw((v) => !v)}>
                            {showIpmiPw ? <IconEyeOff size={12} /> : <IconEye size={12} />}
                          </ActionIcon>
                        )}
                      </Group>
                    </Group>
                  </Stack>
                </div>
              )}
              {hasRedfish && server.bmc?.redfish && (
                <div>
                  <Text size="xs" c="dimmed" fw={600} tt="uppercase" mb={4}>Redfish</Text>
                  <Stack gap={0}>
                    <InfoRow label="Host" value={<Text size="sm" ff="mono">{server.bmc.redfish.host}</Text>} />
                    <InfoRow label="Port" value={server.bmc.redfish.port ? <Text size="sm" ff="mono">{server.bmc.redfish.port}</Text> : '—'} />
                    <InfoRow label="Username" value={server.bmc.redfish.username ?? '—'} />
                    <Group justify="space-between" py={4} style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
                      <Text size="sm" c="dimmed" w={120}>Password</Text>
                      <Group gap="xs" style={{ flex: 1, justifyContent: 'flex-end' }}>
                        <Text size="sm" ff="mono">
                          {server.bmc.redfish.password ? (showRedfishPw ? server.bmc.redfish.password : '••••••••') : '—'}
                        </Text>
                        {server.bmc.redfish.password && (
                          <ActionIcon size="xs" variant="subtle" color="gray" onClick={() => setShowRedfishPw((v) => !v)}>
                            {showRedfishPw ? <IconEyeOff size={12} /> : <IconEye size={12} />}
                          </ActionIcon>
                        )}
                      </Group>
                    </Group>
                  </Stack>
                </div>
              )}
            </Stack>
          )}
        </>
      )}

      {/* ── Edit Mode ── */}
      {isEditing && (
        <Stack gap="sm">
          <Divider label="Edit BMC Configuration" labelPosition="left" />

          {/* IPMI */}
          <Checkbox
            label="Enable IPMI"
            checked={ipmiEnabled}
            onChange={(e) => setIpmiEnabled(e.currentTarget.checked)}
            size="sm"
          />
          {ipmiEnabled && (
            <Stack gap="sm" pl="md">
              <TextInput
                label="IPMI Host / IP"
                value={ipmiHost}
                onChange={(e) => { setIpmiHost(e.target.value); setIpmiHostError(null) }}
                error={ipmiHostError}
                size="xs"
                required
              />
              <Group grow>
                <NumberInput label="Port" value={ipmiPort} onChange={setIpmiPort} min={1} max={65535} size="xs" />
                <TextInput label="Username" value={ipmiUsername} onChange={(e) => setIpmiUsername(e.target.value)} size="xs" />
              </Group>
              <PasswordInput label="Password" value={ipmiPassword} onChange={(e) => setIpmiPassword(e.target.value)} size="xs" />
            </Stack>
          )}

          <Divider variant="dashed" />

          {/* Redfish */}
          <Checkbox
            label="Enable Redfish"
            checked={redfishEnabled}
            onChange={(e) => setRedfishEnabled(e.currentTarget.checked)}
            size="sm"
          />
          {redfishEnabled && (
            <Stack gap="sm" pl="md">
              <TextInput
                label="Redfish Host / IP"
                value={redfishHost}
                onChange={(e) => { setRedfishHost(e.target.value); setRedfishHostError(null) }}
                error={redfishHostError}
                size="xs"
                required
              />
              <Group grow>
                <NumberInput label="Port" value={redfishPort} onChange={setRedfishPort} min={1} max={65535} size="xs" />
                <TextInput label="Username" value={redfishUsername} onChange={(e) => setRedfishUsername(e.target.value)} size="xs" />
              </Group>
              <PasswordInput label="Password" value={redfishPassword} onChange={(e) => setRedfishPassword(e.target.value)} size="xs" />
            </Stack>
          )}

          <Group gap="xs" justify="flex-end">
            <Button size="xs" variant="default" leftSection={<IconX size={12} />} onClick={cancelEdit} disabled={saving}>
              Cancel
            </Button>
            <Button size="xs" loading={saving} onClick={() => void handleSave()}>
              Save
            </Button>
          </Group>
        </Stack>
      )}
    </Card>
  )
}

// ─── AccessSection ────────────────────────────────────────────────────────────

function AccessSection({
  server,
  onUpdate,
}: {
  server: Server
  onUpdate: (patch: Partial<Server>) => Promise<void>
}) {
  return (
    <div>
      <Text fw={600} mb="sm">Remote Access</Text>
      <Grid>
        <Grid.Col span={6}>
          <SshCard server={server} onUpdate={onUpdate} />
        </Grid.Col>
        <Grid.Col span={6}>
          <BmcCard server={server} onUpdate={onUpdate} />
        </Grid.Col>
      </Grid>
    </div>
  )
}

// ─── SystemSettingCard ────────────────────────────────────────────────────────

function SystemSettingCard({
  setting,
  onApply,
}: {
  setting: SystemSetting
  onApply: (key: string, value: boolean, persist: boolean) => Promise<void>
}) {
  const [desiredValue, setDesiredValue] = useState(setting.runtimeValue)
  const [persist, setPersist] = useState(false)
  const [applying, setApplying] = useState(false)
  const [lastResult, setLastResult] = useState<'success' | 'error' | null>(null)

  const isDirty = desiredValue !== setting.runtimeValue || persist

  const handleApply = async () => {
    try {
      setApplying(true)
      setLastResult(null)
      await onApply(setting.key, desiredValue, persist)
      setLastResult('success')
      setPersist(false)
    } catch {
      setLastResult('error')
    } finally {
      setApplying(false)
    }
  }

  return (
    <Card withBorder radius="md">
      <Stack gap="md">
        <Group justify="space-between" align="flex-start">
          <Stack gap={2}>
            <Group gap="xs">
              <Text fw={600} size="sm">{setting.name}</Text>
              <Badge size="xs" variant="outline" color="gray">{setting.category}</Badge>
              {setting.requiresReboot && (
                <Badge size="xs" variant="outline" color="orange">Requires reboot</Badge>
              )}
            </Group>
            <Text size="xs" c="dimmed">{setting.description}</Text>
          </Stack>
        </Group>

        {/* Current state display */}
        <Grid gutter="sm">
          <Grid.Col span={6}>
            <Card withBorder p="sm" style={{ background: 'var(--mantine-color-default-hover)' }}>
              <Text size="xs" c="dimmed" mb={4}>Current (runtime)</Text>
              <Badge
                color={setting.runtimeValue ? 'green' : 'red'}
                variant="light"
                size="sm"
              >
                {setting.runtimeValue ? 'Enabled' : 'Disabled'}
              </Badge>
            </Card>
          </Grid.Col>
          <Grid.Col span={6}>
            <Card withBorder p="sm" style={{ background: 'var(--mantine-color-default-hover)' }}>
              <Text size="xs" c="dimmed" mb={4}>Current (persistent)</Text>
              <Badge
                color={setting.persistentValue ? 'green' : 'red'}
                variant="light"
                size="sm"
              >
                {setting.persistentValue ? 'Enabled' : 'Disabled'}
              </Badge>
            </Card>
          </Grid.Col>
        </Grid>

        {/* Controls */}
        {setting.mutable && (
          <Stack gap="sm">
            <Group justify="space-between" align="center">
              <Stack gap={0}>
                <Text size="sm" fw={500}>Desired state</Text>
                <Text size="xs" c="dimmed">Runtime change takes effect immediately</Text>
              </Stack>
              <Switch
                checked={desiredValue}
                onChange={(e) => { setDesiredValue(e.currentTarget.checked); setLastResult(null) }}
                label={desiredValue ? 'Enabled' : 'Disabled'}
                size="sm"
              />
            </Group>

            <Checkbox
              checked={persist}
              onChange={(e) => { setPersist(e.currentTarget.checked); setLastResult(null) }}
              label="Persist after reboot (writes to /etc/sysctl.conf)"
              size="sm"
            />

            <Group justify="space-between" align="center">
              <div>
                {lastResult === 'success' && (
                  <Group gap={4}>
                    <IconCheck size={14} color="var(--mantine-color-green-6)" />
                    <Text size="xs" c="green">Applied successfully</Text>
                  </Group>
                )}
                {lastResult === 'error' && (
                  <Text size="xs" c="red">Apply failed — check system logs</Text>
                )}
              </div>
              <Button
                size="xs"
                disabled={!isDirty}
                loading={applying}
                onClick={() => void handleApply()}
              >
                Apply
              </Button>
            </Group>
          </Stack>
        )}
      </Stack>
    </Card>
  )
}

// ─── SystemSettingsSection ────────────────────────────────────────────────────

function SystemSettingsSection({
  settings,
  onApply,
}: {
  settings: SystemSetting[]
  onApply: (key: string, value: boolean, persist: boolean) => Promise<void>
}) {
  if (settings.length === 0) {
    return <EmptyState title="No settings available" message="No configurable system settings for this server." />
  }

  return (
    <Stack gap="md">
      <Group gap="xs">
        <IconAdjustments size={16} />
        <Text fw={600}>System Settings</Text>
        <Text size="xs" c="dimmed">Host-level tunables and kernel configuration</Text>
      </Group>
      {settings.map((s) => (
        <SystemSettingCard key={s.key} setting={s} onApply={onApply} />
      ))}
    </Stack>
  )
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export function ServerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { servers, teams, observability } = useApp()

  const [server, setServer] = useState<Server | null>(null)
  const [teamList, setTeamList] = useState<Team[]>([])
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [srv, ts, als] = await Promise.all([
        servers.get.execute(id),
        teams.list.execute(),
        observability.listAlerts.execute({}),
      ])
      if (!srv) { setError('Server not found'); return }
      setServer(srv)
      setTeamList(ts)
      setAlerts(als)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [id, servers, teams, observability])

  useEffect(() => { void load() }, [load])

  const ownerLabel = useMemo(() => {
    if (!server) return '—'
    const state = getAllocationState(server)
    if (state === 'free') return 'Free'
    if (state === 'team') {
      const team = teamList.find((t) => t.id === server.ownerTeamId)
      return team?.name ?? server.ownerTeamId ?? '—'
    }
    return server.ownerUserId ?? '—'
  }, [server, teamList])

  const serverAlerts = useMemo(() => {
    if (!server) return []
    return alerts.filter((a) =>
      a.relatedAssetNames.some((n) => n.toLowerCase().includes(server.hostname.toLowerCase()))
      || a.relatedAssetIds.some((aid) => aid === server.id)
    )
  }, [alerts, server])

  const trendData = useMemo(() => {
    if (!server) return { cpu: [], ram: [], gpu: [] }
    const h = hashId(server.id)
    return {
      cpu: genTrend(h, server.cpuUsagePct, 18),
      ram: genTrend(h + 7, server.ramUsagePct, 12),
      gpu: server.gpuCount > 0 ? genTrend(h + 13, Math.round((server.cpuUsagePct + server.ramUsagePct) / 2), 20) : [],
    }
  }, [server])

  const workloads = useMemo(() => server ? genWorkloads(server) : [], [server])
  const timeline = useMemo(() => server ? genTimeline(server) : [], [server])

  const systemSettings = useMemo((): SystemSetting[] => {
    if (!server) return []
    const h = hashId(server.id)
    return [
      {
        key: 'numa_balancing',
        name: 'NUMA Balancing',
        description: 'Automatic NUMA memory balancing (kernel.numa_balancing). Enables automatic migration of memory to local NUMA nodes.',
        category: 'Memory',
        runtimeValue: (h % 3) !== 0,
        persistentValue: (h % 5) !== 0,
        mutable: true,
        requiresReboot: false,
      },
    ]
  }, [server])

  const notImpl = (action: string) => notifications.show({
    title: 'Not implemented',
    message: `${action} is a UI prototype placeholder`,
    color: 'gray',
  })

  const handleApplySetting = async (key: string, value: boolean, persist: boolean) => {
    await new Promise<void>((resolve) => setTimeout(resolve, 800))
    notImpl(`Apply ${key} = ${String(value)}${persist ? ' (persistent)' : ''}`)
  }

  const handleAccessUpdate = async (patch: Partial<Server>) => {
    await new Promise<void>((resolve) => setTimeout(resolve, 600))
    setServer((prev) => prev ? { ...prev, ...patch } : prev)
    notifications.show({ title: 'Saved', message: 'Access configuration updated', color: 'green' })
  }

  if (loading) return <LoadingState rows={8} />
  if (error || !server) return <ErrorState message={error ?? 'Server not found'} />

  const hasGpu = server.gpuCount > 0

  return (
    <Stack gap="xl">
      {/* ── Section A: Header ─────────────────────────────────────── */}
      <div>
        <Breadcrumbs mb="xs" fz="sm">
          <Anchor size="sm" onClick={() => navigate('/servers')}>Servers</Anchor>
          <Text size="sm">{server.hostname}</Text>
        </Breadcrumbs>

        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Stack gap={6}>
            <Group gap="sm" align="center">
              <Title order={2}>{server.hostname}</Title>
              <StatusBadge status={server.status} />
            </Group>
            <Group gap="xs">
              <Badge variant="dot" color={getAllocationState(server) === 'free' ? 'gray' : 'blue'} size="sm">
                {getAllocationState(server) === 'free'
                  ? 'Unallocated'
                  : getAllocationState(server) === 'team'
                    ? <Group gap={4}><IconUsers size={10} />{ownerLabel}</Group>
                    : <Group gap={4}><IconUser size={10} />{ownerLabel}</Group>
                }
              </Badge>
              <Text size="sm" c="dimmed">{server.ip}</Text>
              {server.location?.datacenter && (
                <Text size="sm" c="dimmed">
                  {[server.location.datacenter, server.location.room, server.location.rack].filter(Boolean).join(' / ')}
                </Text>
              )}
              <Text size="sm" c="dimmed">Last seen {formatRelative(server.lastSeenAt)}</Text>
            </Group>
          </Stack>

          <Group gap="xs">
            <Button size="sm" variant="default" leftSection={<IconRotate size={14} />} onClick={() => notImpl('Reboot')}>
              Reboot
            </Button>
            <Button size="sm" variant="default" leftSection={<IconPower size={14} />} onClick={() => notImpl('Power off')}>
              Power Off
            </Button>
            <Button size="sm" variant="default" leftSection={<IconTool size={14} />} onClick={() => notImpl('Maintenance mode')}>
              Maintenance
            </Button>
          </Group>
        </Group>
      </div>

      {/* ── Tabs ──────────────────────────────────────────────────── */}
      <Tabs defaultValue="summary">
        <Tabs.List>
          <Tabs.Tab value="summary">Summary</Tabs.Tab>
          <Tabs.Tab value="control">Control</Tabs.Tab>
          <Tabs.Tab
            value="workload"
            rightSection={
              workloads.length > 0
                ? <Badge size="xs" variant="filled" color="blue" circle>{workloads.length}</Badge>
                : undefined
            }
          >
            Workload
          </Tabs.Tab>
        </Tabs.List>

        {/* ── Summary Tab ───────────────────────────────────────── */}
        <Tabs.Panel value="summary" pt="md">
          <Stack gap="xl">
            {/* Section E: Remote Access */}
            <AccessSection server={server} onUpdate={handleAccessUpdate} />

            {/* Section B: Resource Usage */}
            <div>
              <Text fw={600} mb="sm">Resource Usage</Text>
              <Group grow mb="md">
                <MetricCard
                  label="CPU Usage"
                  value={`${server.cpuUsagePct}%`}
                  color={server.cpuUsagePct > 80 ? 'red' : server.cpuUsagePct > 60 ? 'orange' : 'blue'}
                  icon={<IconCpu size={14} />}
                />
                <MetricCard
                  label="RAM Usage"
                  value={`${server.ramUsagePct}%`}
                  color={server.ramUsagePct > 80 ? 'red' : server.ramUsagePct > 60 ? 'orange' : 'blue'}
                  icon={<IconDatabase size={14} />}
                />
                <MetricCard
                  label="GPU Usage"
                  value={hasGpu ? `${Math.round((server.cpuUsagePct + server.ramUsagePct) / 2)}%` : '—'}
                  color={hasGpu ? 'violet' : 'gray'}
                  icon={<IconDeviceDesktopAnalytics size={14} />}
                />
              </Group>

              <Grid>
                <Grid.Col span={hasGpu ? 4 : 6}>
                  <TrendPanel label="CPU" color="blue" data={trendData.cpu} icon={<IconCpu size={14} />} />
                </Grid.Col>
                <Grid.Col span={hasGpu ? 4 : 6}>
                  <TrendPanel label="RAM" color="teal" data={trendData.ram} icon={<IconDatabase size={14} />} />
                </Grid.Col>
                {hasGpu && (
                  <Grid.Col span={4}>
                    <TrendPanel label="GPU" color="violet" data={trendData.gpu} icon={<IconDeviceDesktopAnalytics size={14} />} />
                  </Grid.Col>
                )}
              </Grid>
            </div>

            {/* Section C: Alerts + Ops Timeline */}
            <Grid>
              <Grid.Col span={6}>
                <Card withBorder h="100%">
                  <Group gap="xs" mb="md">
                    <IconAlertTriangle size={16} />
                    <Text fw={600}>Alerts</Text>
                    {serverAlerts.length > 0 && (
                      <Badge color="red" size="sm" variant="filled">{serverAlerts.length}</Badge>
                    )}
                  </Group>
                  {serverAlerts.length === 0 ? (
                    <Stack align="center" py="lg" gap="xs">
                      <ThemeIcon color="green" variant="light" size="lg"><IconCheck size={18} /></ThemeIcon>
                      <Text size="sm" c="dimmed">No active alerts</Text>
                    </Stack>
                  ) : (
                    <Stack gap="xs">
                      {serverAlerts.map((alert) => (
                        <Card key={alert.id} withBorder p="xs">
                          <Group justify="space-between" wrap="nowrap">
                            <Stack gap={2} style={{ flex: 1, minWidth: 0 }}>
                              <Group gap="xs">
                                <AlertSeverityBadge severity={alert.severity} />
                                <Badge variant="outline" size="xs" color={alert.status === 'active' ? 'red' : 'gray'}>
                                  {alert.status}
                                </Badge>
                              </Group>
                              <Text size="sm" fw={500} lineClamp={1}>{alert.title}</Text>
                              <Text size="xs" c="dimmed">{formatRelative(alert.createdAt)}</Text>
                            </Stack>
                          </Group>
                        </Card>
                      ))}
                    </Stack>
                  )}
                </Card>
              </Grid.Col>

              <Grid.Col span={6}>
                <Card withBorder h="100%">
                  <Group gap="xs" mb="md">
                    <IconClock size={16} />
                    <Text fw={600}>Operations Timeline</Text>
                  </Group>
                  <ScrollArea h={260}>
                    <Timeline active={0} bulletSize={24} lineWidth={2}>
                      {timeline.map((ev, i) => (
                        <Timeline.Item
                          key={i}
                          bullet={<ThemeIcon size={20} variant="light" color="gray">{ev.icon}</ThemeIcon>}
                          title={<Text size="sm" fw={500}>{ev.title}</Text>}
                        >
                          <Group gap="xs">
                            <Text size="xs" c="dimmed">{ev.actor}</Text>
                            <Text size="xs" c="dimmed">·</Text>
                            <Text size="xs" c="dimmed">{formatRelative(ev.time)}</Text>
                          </Group>
                        </Timeline.Item>
                      ))}
                    </Timeline>
                  </ScrollArea>
                </Card>
              </Grid.Col>
            </Grid>

            {/* Section D: Server Information */}
            <div>
              <Text fw={600} mb="sm">Server Information</Text>
              <Grid>
                <Grid.Col span={6}>
                  <Stack gap="md">
                    <Card withBorder>
                      <Group gap="xs" mb="sm">
                        <IconServer size={14} />
                        <Text size="sm" fw={600}>Location</Text>
                      </Group>
                      <Stack gap={0}>
                        <InfoRow label="Datacenter" value={server.location?.datacenter ?? '—'} />
                        <InfoRow label="Room" value={server.location?.room ?? '—'} />
                        <InfoRow label="Rack" value={server.location?.rack ?? '—'} />
                      </Stack>
                    </Card>
                  </Stack>
                </Grid.Col>

                <Grid.Col span={6}>
                  <Stack gap="md">
                    <Card withBorder>
                      <Group gap="xs" mb="sm">
                        <IconCpu size={14} />
                        <Text size="sm" fw={600}>Hardware</Text>
                      </Group>
                      <Stack gap={0}>
                        <InfoRow label="CPU Cores" value={`${server.cpuCores} cores`} />
                        <InfoRow label="RAM" value={`${server.ramGB} GB`} />
                        {hasGpu ? (
                          <>
                            <InfoRow label="GPU Model" value={server.gpuType} />
                            <InfoRow label="GPU Count" value={`${server.gpuCount}x`} />
                          </>
                        ) : (
                          <InfoRow label="GPU" value="None" />
                        )}
                      </Stack>
                    </Card>

                    <Card withBorder>
                      <Group gap="xs" mb="sm">
                        <IconDatabase size={14} />
                        <Text size="sm" fw={600}>System</Text>
                      </Group>
                      <Stack gap={0}>
                        <InfoRow label="OS" value={server.os ?? '—'} />
                        <InfoRow label="Owner" value={ownerLabel} />
                        <InfoRow label="Last Seen" value={formatRelative(server.lastSeenAt)} />
                        <InfoRow label="Registered" value={formatDateTime(server.createdAt)} />
                      </Stack>
                    </Card>
                  </Stack>
                </Grid.Col>
              </Grid>
            </div>

          </Stack>
        </Tabs.Panel>

        {/* ── Control Tab ───────────────────────────────────────── */}
        <Tabs.Panel value="control" pt="md">
          <SystemSettingsSection
            settings={systemSettings}
            onApply={handleApplySetting}
          />
        </Tabs.Panel>

        {/* ── Workload Tab ──────────────────────────────────────── */}
        <Tabs.Panel value="workload" pt="md">
          <Stack gap="md">
            <Group justify="space-between">
              <Group gap="sm">
                <Text fw={600}>Containers</Text>
                <Badge variant="outline" size="sm" color="gray">{workloads.length} containers</Badge>
              </Group>
              <Button size="xs" leftSection={<IconPlus size={12} />}
                onClick={() => navigate(`/servers/${server.id}/containers/new`)}>
                Create Container
              </Button>
            </Group>

            {workloads.length === 0 ? (
              <EmptyState title="No containers" message="No containers running on this server." />
            ) : (
              <Card withBorder p={0}>
                <Table highlightOnHover>
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Name</Table.Th>
                      <Table.Th>Image</Table.Th>
                      <Table.Th>State</Table.Th>
                      <Table.Th>Restarts</Table.Th>
                      <Table.Th>Started</Table.Th>
                      <Table.Th />
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {workloads.map((wl) => (
                      <Table.Tr key={wl.id}>
                        <Table.Td>
                          <Text size="sm" fw={500} ff="mono">{wl.name}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="xs" c="dimmed" ff="mono" style={{ maxWidth: 240, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {wl.image}
                          </Text>
                        </Table.Td>
                        <Table.Td><WorkloadStateBadge state={wl.state} /></Table.Td>
                        <Table.Td>
                          <Text size="sm" c={wl.restartCount > 2 ? 'red' : 'inherit'}>{wl.restartCount}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" c="dimmed">{formatRelative(wl.startedAt)}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Group gap={4} justify="flex-end">
                            <Button
                              size="xs"
                              variant="subtle"
                              color="green"
                              leftSection={<IconPlayerPlay size={12} />}
                              onClick={() => notImpl('Start')}
                              disabled={wl.state === 'running' || wl.state === 'restarting'}
                            >
                              Start
                            </Button>
                            <Button
                              size="xs"
                              variant="subtle"
                              leftSection={<IconRefresh size={12} />}
                              onClick={() => notImpl('Restart')}
                              disabled={wl.state !== 'running'}
                            >
                              Restart
                            </Button>
                            <Button
                              size="xs"
                              variant="subtle"
                              color="red"
                              leftSection={<IconServerOff size={12} />}
                              onClick={() => notImpl('Stop')}
                              disabled={wl.state !== 'running'}
                            >
                              Stop
                            </Button>
                          </Group>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Card>
            )}
          </Stack>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}
