import { useMemo, useState } from 'react'
import { Button, Field, Input, SimpleGrid, Stack, Textarea } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import {
  DOCKER_OBJECT_NAME_PATTERN,
  parseEnvLines,
  parsePortLines,
  parseVolumeLines,
  splitCommandLine,
  type CreateDockerContainerInput,
  type DockerRestartPolicy,
} from '@/domain/software/docker'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'

const RESTART_POLICIES: ReadonlyArray<{ value: DockerRestartPolicy; label: string }> = [
  { value: 'no', label: 'No (default)' },
  { value: 'unless-stopped', label: 'Unless stopped' },
  { value: 'always', label: 'Always' },
  { value: 'on-failure', label: 'On failure' },
]

interface CreateContainerDialogProps {
  serverId: string
  onClose: () => void
  /** Called after the container was created (and started, when requested). */
  onCreated: () => void
}

/**
 * Create-container form for the Docker Host Explorer.
 *
 * The fields follow `docker run` so operators can reuse what they know: ports as
 * `[hostIp:][hostPort:]containerPort[/protocol]` and mounts as `source:/target[:ro]`, one per line;
 * the command is split into arguments honouring quotes. Each multi-line field is parsed as it is
 * typed and an offending line is reported under the field, so submission is blocked until the
 * request satisfies the contract; the Engine stays the authority (for example on whether the image
 * is present or the name is free) and its message is shown inline. Local image tags and network
 * names are offered as suggestions, loaded once when the dialog opens. When the container is
 * created but cannot start, the inline error says so and the list refreshes on close, because the
 * container exists.
 */
export function CreateContainerDialog({ serverId, onClose, onCreated }: CreateContainerDialogProps) {
  const { dockerHosts } = useApp()
  const { showToast } = useToast()
  const images = useAsyncData(() => dockerHosts.listImages(serverId), [serverId])
  const networks = useAsyncData(() => dockerHosts.listNetworks(serverId), [serverId])
  const [image, setImage] = useState('')
  const [name, setName] = useState('')
  const [command, setCommand] = useState('')
  const [env, setEnv] = useState('')
  const [ports, setPorts] = useState('')
  const [volumes, setVolumes] = useState('')
  const [network, setNetwork] = useState('')
  const [restartPolicy, setRestartPolicy] = useState<DockerRestartPolicy>('no')
  const [start, setStart] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  // Set once a create succeeded but the start failed: the container exists, so closing must refresh.
  const [createdButNotStarted, setCreatedButNotStarted] = useState(false)

  const parsedEnv = useMemo(() => parseEnvLines(env), [env])
  const parsedPorts = useMemo(() => parsePortLines(ports), [ports])
  const parsedVolumes = useMemo(() => parseVolumeLines(volumes), [volumes])
  const nameInvalid = name.trim() !== '' && !DOCKER_OBJECT_NAME_PATTERN.test(name.trim())
  const valid = image.trim() !== '' && !nameInvalid && parsedEnv.ok && parsedPorts.ok && parsedVolumes.ok

  const imageSuggestions = images.status === 'ready' ? images.data.flatMap((item) => item.repoTags) : []
  const networkSuggestions = networks.status === 'ready' ? networks.data.map((item) => item.name) : []

  const close = () => {
    if (submitting) return
    if (createdButNotStarted) onCreated()
    else onClose()
  }

  const submit = async () => {
    if (!valid || submitting || !parsedEnv.ok || !parsedPorts.ok || !parsedVolumes.ok) return
    const input: CreateDockerContainerInput = {
      image: image.trim(),
      ...(name.trim() ? { name: name.trim() } : {}),
      ...(command.trim() ? { command: splitCommandLine(command) } : {}),
      env: parsedEnv.values,
      ports: parsedPorts.values,
      volumes: parsedVolumes.values,
      ...(network.trim() ? { network: network.trim() } : {}),
      restartPolicy,
      start,
    }
    setSubmitting(true)
    setError('')
    try {
      const created = await dockerHosts.createContainer(serverId, input)
      showToast({
        tone: 'success',
        title: created.started ? 'Container created and started' : 'Container created',
        description: created.warnings.length > 0 ? created.warnings.join(' ') : input.name ?? created.id.slice(0, 12),
      })
      onCreated()
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : 'The container could not be created.'
      if (message.includes('created but could not be started')) setCreatedButNotStarted(true)
      setError(message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      size="lg"
      title="Create container"
      description="Create a container from an image already on this host, like docker run."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            {createdButNotStarted ? 'Close' : 'Cancel'}
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!valid || submitting}>
            {start ? 'Create and start' : 'Create'}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The container could not be created">
            {error}
          </Alert>
        )}
        <SimpleGrid columns={{ base: 1, md: 2 }} gap="4">
          <Field.Root required>
            <Field.Label>
              Image <Field.RequiredIndicator />
            </Field.Label>
            <Input value={image} onChange={(event) => setImage(event.target.value)} placeholder="nginx:1.27" list="docker-image-suggestions" autoFocus />
            <datalist id="docker-image-suggestions">
              {imageSuggestions.map((tag) => (
                <option key={tag} value={tag} />
              ))}
            </datalist>
            <Field.HelperText>Must already be on this host; pull it first from the Images section.</Field.HelperText>
          </Field.Root>
          <Field.Root invalid={nameInvalid}>
            <Field.Label>Name</Field.Label>
            <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="web" />
            <Field.HelperText>Optional; the Engine generates one when empty.</Field.HelperText>
            <Field.ErrorText>Use letters, digits, and _ . - (starting with a letter or digit).</Field.ErrorText>
          </Field.Root>
        </SimpleGrid>
        <Field.Root>
          <Field.Label>Command</Field.Label>
          <Input value={command} onChange={(event) => setCommand(event.target.value)} placeholder={'nginx -g "daemon off;"'} className="mono" />
          <Field.HelperText>Optional; replaces the image&apos;s default command. Quotes group arguments.</Field.HelperText>
        </Field.Root>
        <Field.Root invalid={!parsedPorts.ok}>
          <Field.Label>Published ports</Field.Label>
          <Textarea value={ports} onChange={(event) => setPorts(event.target.value)} rows={2} className="mono" placeholder={'8080:80\n53/udp'} />
          <Field.HelperText>One per line: [hostIp:][hostPort:]containerPort[/tcp|udp|sctp].</Field.HelperText>
          {!parsedPorts.ok && <Field.ErrorText>{parsedPorts.error}</Field.ErrorText>}
        </Field.Root>
        <Field.Root invalid={!parsedVolumes.ok}>
          <Field.Label>Volumes</Field.Label>
          <Textarea value={volumes} onChange={(event) => setVolumes(event.target.value)} rows={2} className="mono" placeholder={'web-data:/usr/share/nginx/html\n/srv/conf:/etc/nginx/conf.d:ro'} />
          <Field.HelperText>One per line: volume-name:/path or /host/path:/path, optionally :ro.</Field.HelperText>
          {!parsedVolumes.ok && <Field.ErrorText>{parsedVolumes.error}</Field.ErrorText>}
        </Field.Root>
        <Field.Root invalid={!parsedEnv.ok}>
          <Field.Label>Environment</Field.Label>
          <Textarea value={env} onChange={(event) => setEnv(event.target.value)} rows={2} className="mono" placeholder={'TZ=Asia/Taipei'} />
          <Field.HelperText>One KEY=value per line.</Field.HelperText>
          {!parsedEnv.ok && <Field.ErrorText>{parsedEnv.error}</Field.ErrorText>}
        </Field.Root>
        <SimpleGrid columns={{ base: 1, md: 2 }} gap="4">
          <Field.Root>
            <Field.Label>Network</Field.Label>
            <Input value={network} onChange={(event) => setNetwork(event.target.value)} placeholder="bridge" list="docker-network-suggestions" />
            <datalist id="docker-network-suggestions">
              {networkSuggestions.map((item) => (
                <option key={item} value={item} />
              ))}
            </datalist>
          </Field.Root>
          <Field.Root>
            <Field.Label>Restart policy</Field.Label>
            <Select
              aria-label="Restart policy"
              value={restartPolicy}
              onChange={(value) => setRestartPolicy(value as DockerRestartPolicy)}
              options={RESTART_POLICIES.map((option) => ({ value: option.value, label: option.label }))}
            />
          </Field.Root>
        </SimpleGrid>
        <Checkbox checked={start} onCheckedChange={setStart}>
          Start the container after creating it
        </Checkbox>
      </Stack>
    </Modal>
  )
}
