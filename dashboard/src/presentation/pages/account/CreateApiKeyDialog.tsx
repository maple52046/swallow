import { useState } from 'react'
import { Box, Button, Field, Input, InputGroup, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { CodeBlock } from '@/presentation/components/CodeBlock'
import { CopyButton } from '@/presentation/components/CopyButton'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import {
  API_KEY_EXPIRY_OPTIONS,
  DEFAULT_API_KEY_EXPIRY,
  expiresAtFor,
  type ApiKeyExpiryChoice,
} from './apiKeyPresentation'

/** Where the saved secret goes; it never echoes the secret itself into a command line. */
const CLI_EXAMPLE = [
  '# Store the key in your CLI profile',
  'swallow login --api-key-stdin < ci.key',
  '',
  '# Or pass it for a single command',
  'SWALLOW_API_KEY="$(cat ci.key)" swallow servers list',
].join('\n')

interface CreateApiKeyDialogProps {
  onClose: () => void
  /** Called once the operator has confirmed saving the secret, with the toast title to show. */
  onCreated: (title: string) => void
}

/** The secret held only in this dialog's memory between creation and confirmation. */
interface CreatedSecret {
  name: string
  secret: string
}

/**
 * Creates an API Key for the signed-in admin and shows its secret once (decision 042).
 *
 * swallow keeps only a hash of the secret, so this dialog is the only place it ever exists outside
 * the operator's tooling. Security and state contract:
 * - The secret lives only in component state; it is never written to storage or logged, and it is
 *   dropped when the dialog unmounts.
 * - After creation the dialog cannot be dismissed (Escape, backdrop, close button) until the
 *   operator confirms they saved the secret, because a dismissal would lose it irrecoverably.
 * - The usage examples show where the secret goes without echoing it into a command line.
 */
export function CreateApiKeyDialog({ onClose, onCreated }: CreateApiKeyDialogProps) {
  const { apiKeys } = useApp()
  const [name, setName] = useState('')
  const [expiry, setExpiry] = useState<ApiKeyExpiryChoice>(DEFAULT_API_KEY_EXPIRY)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [created, setCreated] = useState<CreatedSecret | null>(null)
  const [saved, setSaved] = useState(false)

  const close = () => {
    if (submitting) return
    // Once a key exists, only the explicit "Done" after confirmation may close the dialog.
    if (created && !saved) return
    if (created) {
      onCreated('API key created')
      return
    }
    onClose()
  }

  const create = async () => {
    if (submitting || !name.trim()) return
    setSubmitting(true)
    setError('')
    try {
      const result = await apiKeys.createApiKey({ name: name.trim(), expiresAt: expiresAtFor(expiry, Date.now()) })
      setCreated({ name: result.key.name, secret: result.secret })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The API key could not be created.')
    } finally {
      setSubmitting(false)
    }
  }

  if (created) {
    return (
      <Modal
        open
        onClose={close}
        closeOnInteractOutside={false}
        title="Save your API key"
        description="This is the only time the key is shown. Swallow keeps only a hash of it."
        footer={
          <Button colorPalette="brand" onClick={close} disabled={!saved}>
            Done
          </Button>
        }
      >
        <Stack gap="4">
          <Alert status="warning" title="It cannot be shown again">
            Copy the key now and store it in your password manager or CI secret store. If you lose it, delete this key and create a new one.
          </Alert>
          <Field.Root>
            <Field.Label htmlFor="api-key-created-secret">API key {created.name}</Field.Label>
            {/* The copy action sits inside the field's trailing edge, next to what it copies. */}
            <InputGroup endElement={<CopyButton value={created.secret} label="Copy API key" />} endElementProps={{ pe: '1' }}>
              <Input id="api-key-created-secret" value={created.secret} readOnly className="sw-mono" spellCheck={false} />
            </InputGroup>
          </Field.Root>
          <Box>
            <Text fontWeight="medium" fontSize="sm" mb="1">Use it with the CLI</Text>
            <CodeBlock code={CLI_EXAMPLE} language="bash" aria-label="CLI example" />
          </Box>
          <Checkbox checked={saved} onCheckedChange={setSaved}>
            I have saved the API key
          </Checkbox>
        </Stack>
      </Modal>
    )
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Create API key"
      description="An API key lets scripts, CI, and the CLI call Swallow as you without a password."
      onSubmit={(event) => {
        event.preventDefault()
        void create()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={submitting || !name.trim()}>
            Create
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="API key could not be created">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="api-key-create-name">
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input
            id="api-key-create-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={64}
            placeholder="e.g. ci-runner"
            disabled={submitting}
            autoFocus
          />
          <Field.HelperText>Unique among your keys, so you can tell where each one is used.</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="api-key-create-expiry">Expires</Field.Label>
          <Select
            id="api-key-create-expiry"
            aria-label="Expires"
            value={expiry}
            onChange={(value) => setExpiry(value as ApiKeyExpiryChoice)}
            options={API_KEY_EXPIRY_OPTIONS}
            disabled={submitting}
          />
        </Field.Root>
        <Text color="fg.muted" fontSize="sm">
          The key acts with your permissions. Delete it at any time to revoke it immediately.
        </Text>
      </Stack>
    </Modal>
  )
}
