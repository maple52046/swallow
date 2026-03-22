import { useEffect, useRef, useState } from 'react'
import {
  Alert, Button, Card, Checkbox, Divider, Group, NumberInput,
  PasswordInput, Select, Stack, Text, TextInput, Title,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import { IconAlertCircle, IconArrowLeft, IconPlus } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import type { Datacenter, Room, Rack } from '@/domain/topology/types'
import type { Team } from '@/domain/team/types'
import type { CreateServerInput, ServerBMC, ServerLocation, ServerSSH } from '@/domain/server/types'

const IPV4_RE = /^\d{1,3}(\.\d{1,3}){3}$/

function validateIPv4(ip: string): boolean {
  if (!IPV4_RE.test(ip)) return false
  return ip.split('.').every((part) => {
    const n = Number(part)
    return n >= 0 && n <= 255
  })
}

export function AddServerPage() {
  const { servers, topology, teams: teamsUseCase } = useApp()
  const navigate = useNavigate()

  // --- Basic ---
  const [hostname, setHostname] = useState('')
  const [ip, setIp] = useState('')
  const [os, setOs] = useState('')

  // --- Location ---
  const [datacenters, setDatacenters] = useState<Datacenter[]>([])
  const [rooms, setRooms] = useState<Room[]>([])
  const [racks, setRacks] = useState<Rack[]>([])
  const [dcId, setDcId] = useState<string | null>(null)
  const [roomId, setRoomId] = useState<string | null>(null)
  const [rackId, setRackId] = useState<string | null>(null)

  // --- Owner ---
  const [teamList, setTeamList] = useState<Team[]>([])
  const [ownerTeamId, setOwnerTeamId] = useState<string | null>(null)

  // --- BMC ---
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

  // --- SSH ---
  const [sshHost, setSshHost] = useState('')
  const sshHostManuallyChanged = useRef(false)
  const [sshPort, setSshPort] = useState<number | string>(22)
  const [keyAuthEnabled, setKeyAuthEnabled] = useState(false)
  const [passwordAuthEnabled, setPasswordAuthEnabled] = useState(false)
  const [sshLoginUser, setSshLoginUser] = useState('')
  const [sshPassword, setSshPassword] = useState('')

  // --- Validation errors ---
  const [hostnameError, setHostnameError] = useState<string | null>(null)
  const [ipError, setIpError] = useState<string | null>(null)
  const [ipmiHostError, setIpmiHostError] = useState<string | null>(null)
  const [redfishHostError, setRedfishHostError] = useState<string | null>(null)
  const [sshLoginUserError, setSshLoginUserError] = useState<string | null>(null)
  const [sshPasswordError, setSshPasswordError] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)

  const [submitting, setSubmitting] = useState(false)

  // Load datacenters + teams on mount
  useEffect(() => {
    void topology.datacenters.list.execute().then(setDatacenters)
    void teamsUseCase.list.execute().then(setTeamList)
  }, [topology, teamsUseCase])

  // Load rooms when DC changes
  useEffect(() => {
    setRoomId(null)
    setRackId(null)
    if (dcId) {
      void topology.rooms.list.execute({ datacenterId: dcId }).then(setRooms)
      void topology.racks.list.execute({ datacenterId: dcId }).then(setRacks)
    } else {
      setRooms([])
      setRacks([])
    }
  }, [dcId, topology])

  // Filter racks when room changes
  useEffect(() => {
    setRackId(null)
    if (dcId) {
      void topology.racks.list.execute({ datacenterId: dcId, roomId: roomId ?? undefined }).then(setRacks)
    }
  }, [roomId, dcId, topology])

  // Auto-fill SSH host from server IP (unless manually changed)
  useEffect(() => {
    if (!sshHostManuallyChanged.current) {
      setSshHost(ip)
    }
  }, [ip])

  const validate = (): boolean => {
    let valid = true

    if (!hostname.trim()) { setHostnameError('Hostname is required'); valid = false } else setHostnameError(null)

    const trimIp = ip.trim()
    if (!trimIp) { setIpError('IP address is required'); valid = false }
    else if (!validateIPv4(trimIp)) { setIpError('Must be a valid IPv4 address (e.g. 10.0.1.100)'); valid = false }
    else setIpError(null)

    if (ipmiEnabled && !ipmiHost.trim()) { setIpmiHostError('IPMI host is required when IPMI is enabled'); valid = false } else setIpmiHostError(null)
    if (redfishEnabled && !redfishHost.trim()) { setRedfishHostError('Redfish host is required when Redfish is enabled'); valid = false } else setRedfishHostError(null)

    if (passwordAuthEnabled) {
      if (!sshLoginUser.trim()) { setSshLoginUserError('Login user is required when password authentication is enabled'); valid = false } else setSshLoginUserError(null)
      if (!sshPassword.trim()) { setSshPasswordError('Password is required when password authentication is enabled'); valid = false } else setSshPasswordError(null)
    } else {
      setSshLoginUserError(null)
      setSshPasswordError(null)
    }

    return valid
  }

  const buildPayload = (): CreateServerInput => {
    let location: ServerLocation | undefined
    if (dcId) {
      const dc = datacenters.find((d) => d.id === dcId)
      const room = roomId ? rooms.find((r) => r.id === roomId) : undefined
      const rack = rackId ? racks.find((r) => r.id === rackId) : undefined
      location = {
        datacenter: dc?.name,
        room: room?.name,
        rack: rack?.name,
        datacenterId: dcId,
        roomId: roomId ?? undefined,
        rackId: rackId ?? undefined,
      }
    }

    let bmc: ServerBMC | undefined
    if (ipmiEnabled || redfishEnabled) {
      bmc = {
        address: ipmiEnabled ? ipmiHost.trim() : (redfishEnabled ? redfishHost.trim() : ''),
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
      }
    }

    let ssh: ServerSSH | undefined
    if (sshHost.trim()) {
      ssh = {
        host: sshHost.trim(),
        port: typeof sshPort === 'number' ? sshPort : 22,
        username: sshLoginUser.trim() || '',
        keyAuthEnabled,
        passwordAuthEnabled,
        password: passwordAuthEnabled ? sshPassword.trim() : undefined,
      }
    }

    return {
      hostname: hostname.trim(),
      ip: ip.trim(),
      os: os.trim() || undefined,
      ownerTeamId: ownerTeamId || null,
      location,
      bmc,
      ssh,
    }
  }

  const handleSubmit = async () => {
    setFormError(null)
    if (!validate()) return
    try {
      setSubmitting(true)
      await servers.create.execute(buildPayload())
      notifications.show({
        title: 'Server created',
        message: `${hostname.trim()} has been added to the server list`,
        color: 'green',
      })
      navigate('/servers')
    } catch (e) {
      setFormError(String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const dcOptions = datacenters.map((d) => ({ value: d.id, label: d.name }))
  const roomOptions = rooms.map((r) => ({ value: r.id, label: r.name }))
  const rackOptions = racks.map((r) => ({
    value: r.id,
    label: r.roomId ? r.name : `${r.name} (no room)`,
  }))
  const teamOptions = teamList.map((t) => ({ value: t.id, label: t.name }))

  return (
    <>
      <PageHeader
        title="Add Server"
        subtitle="Manually create a server record with basic information"
        actions={
          <Button
            variant="subtle"
            leftSection={<IconArrowLeft size={14} />}
            onClick={() => navigate('/servers')}
          >
            Back to Servers
          </Button>
        }
      />

      <Stack gap="lg" maw={720}>
        {formError && (
          <Alert icon={<IconAlertCircle size={16} />} color="red" variant="light">
            {formError}
          </Alert>
        )}

        {/* Section 1: Basic Information */}
        <Card withBorder radius="md">
          <Title order={5} mb={4}>Basic Information</Title>
          <Text size="xs" c="dimmed" mb="md">
            Required fields to register the server. All other fields are optional and can be filled in later.
          </Text>
          <Stack gap="md">
            <TextInput
              label="Hostname"
              placeholder="e.g. gpu-node-013"
              value={hostname}
              onChange={(e) => { setHostname(e.target.value); setHostnameError(null) }}
              error={hostnameError}
              required
            />
            <TextInput
              label="IP Address"
              placeholder="e.g. 10.0.1.100"
              value={ip}
              onChange={(e) => { setIp(e.target.value); setIpError(null) }}
              error={ipError}
              required
            />
            <TextInput
              label="Operating System"
              placeholder="e.g. Ubuntu 22.04 LTS"
              value={os}
              onChange={(e) => setOs(e.target.value)}
            />
          </Stack>
        </Card>

        {/* Section 2: Location */}
        <Card withBorder radius="md">
          <Title order={5} mb={4}>Location <Text component="span" size="xs" c="dimmed">(Optional)</Text></Title>
          <Text size="xs" c="dimmed" mb="md">
            Assign this server to a physical location in the datacenter topology.
          </Text>
          <Stack gap="md">
            <Select
              label="Datacenter"
              placeholder="Select datacenter"
              data={dcOptions}
              value={dcId}
              onChange={(v) => { setDcId(v); setRoomId(null); setRackId(null) }}
              clearable
            />
            <Select
              label="Room"
              placeholder={dcId ? 'Select room (optional)' : 'Select a datacenter first'}
              data={roomOptions}
              value={roomId}
              onChange={(v) => { setRoomId(v); setRackId(null) }}
              disabled={!dcId}
              clearable
            />
            <Select
              label="Rack"
              placeholder={dcId ? 'Select rack (optional)' : 'Select a datacenter first'}
              data={rackOptions}
              value={rackId}
              onChange={setRackId}
              disabled={!dcId}
              clearable
            />
          </Stack>
        </Card>

        {/* Section 3: Owner */}
        <Card withBorder radius="md">
          <Title order={5} mb={4}>Owner <Text component="span" size="xs" c="dimmed">(Optional)</Text></Title>
          <Text size="xs" c="dimmed" mb="md">
            Assign this server to a team.
          </Text>
          <Select
            label="Team"
            placeholder="No owner (free)"
            data={teamOptions}
            value={ownerTeamId}
            onChange={setOwnerTeamId}
            clearable
          />
        </Card>

        {/* Section 4: BMC / Management Interfaces */}
        <Card withBorder radius="md">
          <Title order={5} mb={4}>BMC / Management Interfaces <Text component="span" size="xs" c="dimmed">(Optional)</Text></Title>
          <Text size="xs" c="dimmed" mb="md">
            Configure out-of-band management interfaces. IPMI and Redfish can both be enabled independently.
          </Text>
          <Stack gap="lg">
            {/* IPMI */}
            <div>
              <Checkbox
                label="Enable IPMI"
                checked={ipmiEnabled}
                onChange={(e) => setIpmiEnabled(e.currentTarget.checked)}
                mb={ipmiEnabled ? 'md' : 0}
              />
              {ipmiEnabled && (
                <Stack gap="sm" pl="xl">
                  <TextInput
                    label="IPMI Host / IP"
                    placeholder="e.g. 10.0.10.1"
                    value={ipmiHost}
                    onChange={(e) => { setIpmiHost(e.target.value); setIpmiHostError(null) }}
                    error={ipmiHostError}
                    required
                  />
                  <Group grow>
                    <NumberInput
                      label="Port"
                      value={ipmiPort}
                      onChange={setIpmiPort}
                      min={1}
                      max={65535}
                    />
                    <TextInput
                      label="Username"
                      placeholder="e.g. ADMIN"
                      value={ipmiUsername}
                      onChange={(e) => setIpmiUsername(e.target.value)}
                    />
                  </Group>
                  <PasswordInput
                    label="Password"
                    value={ipmiPassword}
                    onChange={(e) => setIpmiPassword(e.target.value)}
                  />
                </Stack>
              )}
            </div>

            <Divider variant="dashed" />

            {/* Redfish */}
            <div>
              <Checkbox
                label="Enable Redfish"
                checked={redfishEnabled}
                onChange={(e) => setRedfishEnabled(e.currentTarget.checked)}
                mb={redfishEnabled ? 'md' : 0}
              />
              {redfishEnabled && (
                <Stack gap="sm" pl="xl">
                  <TextInput
                    label="Redfish Host / IP"
                    placeholder="e.g. 10.0.10.1"
                    value={redfishHost}
                    onChange={(e) => { setRedfishHost(e.target.value); setRedfishHostError(null) }}
                    error={redfishHostError}
                    required
                  />
                  <Group grow>
                    <NumberInput
                      label="Port"
                      value={redfishPort}
                      onChange={setRedfishPort}
                      min={1}
                      max={65535}
                    />
                    <TextInput
                      label="Username"
                      placeholder="e.g. admin"
                      value={redfishUsername}
                      onChange={(e) => setRedfishUsername(e.target.value)}
                    />
                  </Group>
                  <PasswordInput
                    label="Password"
                    value={redfishPassword}
                    onChange={(e) => setRedfishPassword(e.target.value)}
                  />
                </Stack>
              )}
            </div>
          </Stack>
        </Card>

        {/* Section 5: SSH */}
        <Card withBorder radius="md">
          <Title order={5} mb={4}>SSH <Text component="span" size="xs" c="dimmed">(Optional)</Text></Title>
          <Text size="xs" c="dimmed" mb="md">
            SSH connection settings. Host defaults to the server IP. Authentication modes are independent.
          </Text>
          <Stack gap="md">
            <Group grow align="flex-start">
              <TextInput
                label="SSH Host / IP"
                placeholder="Defaults to server IP"
                value={sshHost}
                onChange={(e) => {
                  sshHostManuallyChanged.current = true
                  setSshHost(e.target.value)
                }}
              />
              <NumberInput
                label="Port"
                value={sshPort}
                onChange={setSshPort}
                min={1}
                max={65535}
              />
            </Group>

            <Stack gap="sm">
              <Checkbox
                label="Enable key authentication"
                checked={keyAuthEnabled}
                onChange={(e) => setKeyAuthEnabled(e.currentTarget.checked)}
              />
              <Checkbox
                label="Enable password authentication"
                checked={passwordAuthEnabled}
                onChange={(e) => setPasswordAuthEnabled(e.currentTarget.checked)}
              />
            </Stack>

            {passwordAuthEnabled && (
              <Stack gap="sm" pl="xl">
                <TextInput
                  label="Login User"
                  placeholder="e.g. ubuntu"
                  value={sshLoginUser}
                  onChange={(e) => { setSshLoginUser(e.target.value); setSshLoginUserError(null) }}
                  error={sshLoginUserError}
                  required
                />
                <PasswordInput
                  label="Password"
                  value={sshPassword}
                  onChange={(e) => { setSshPassword(e.target.value); setSshPasswordError(null) }}
                  error={sshPasswordError}
                  required
                />
              </Stack>
            )}
          </Stack>
        </Card>

        <Divider />

        <Group justify="flex-end">
          <Button
            variant="default"
            onClick={() => navigate('/servers')}
            disabled={submitting}
          >
            Cancel
          </Button>
          <Button
            leftSection={<IconPlus size={14} />}
            loading={submitting}
            onClick={() => void handleSubmit()}
          >
            Create Server
          </Button>
        </Group>
      </Stack>
    </>
  )
}
