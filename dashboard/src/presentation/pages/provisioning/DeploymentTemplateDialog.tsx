import { Button, Field, Heading, Input, Stack, Switch, Text, Textarea } from '@chakra-ui/react'
import { ArrowLeft, ArrowRight, Cloud } from 'lucide-react'
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import type { Pool } from '@/domain/infrastructure/types'
import type {
  DeploymentNetworkMode,
  DeploymentNetworkSettings,
  DeploymentTemplate,
  MinimumHardware,
  UpdateDeploymentTemplateInput,
} from '@/domain/provisioning/types'
import type { ProvisionerImageCatalog } from '@/domain/site/types'
import type { User } from '@/domain/user/types'
import { isAdmin, poolRoleOf } from '@/domain/user/access'
import { useApp } from '@/di/AppProvider'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { deployTargetForEphemeral, deployTargetIsEphemeral } from '@/domain/provisioning/types'
import { AddressingModeField } from './AddressingModeField'
import { DeployTargetField } from './DeployTargetField'
import { OSImagePicker } from './OSImagePicker'
import { defaultDeployTargetForImage } from './osDeploymentFlow'
import {
  EMPTY_MINIMUM_HARDWARE_DRAFT,
  minimumHardwareDraft,
  minimumHardwareIsEmpty,
  parseMinimumHardware,
  type MinimumHardwareDraft,
} from './minimumHardwareForm'

/** The catalog action that opens the editor with either a blank or stored Template. */
export type DeploymentTemplateEditorTarget =
  | { kind: 'create'; integrationId: string; imageId: string }
  | { kind: 'edit'; template: DeploymentTemplate }

interface DeploymentTemplateDialogProps {
  target: DeploymentTemplateEditorTarget
  catalogs: readonly ProvisionerImageCatalog[]
  pools: readonly Pool[]
  currentUser: User | null
  onClose: () => void
  /** Reloads cards after metadata or secret state changed without making the dialog own list state. */
  onChanged: () => Promise<void>
}

interface TemplateDraft {
  integrationId: string
  poolId: string
  name: string
  description: string
  imageId: string
  ephemeral: boolean
  networkMode: DeploymentNetworkMode
  subnetId: string
  defaultGateway: boolean
  minimumEnabled: boolean
  minimum: MinimumHardwareDraft
  userDataEnabled: boolean
  userData: string
}

/** Fields of the Platform-node hardware floor; memory is edited in GiB and converted on submit. */
const MINIMUM_FIELDS: ReadonlyArray<{ key: keyof MinimumHardwareDraft; label: string; numeric: boolean; helper: string }> = [
  { key: 'cpuCores', label: 'Minimum CPU cores', numeric: true, helper: 'Physical cores from the Server inventory.' },
  { key: 'memoryGiB', label: 'Minimum memory (GiB)', numeric: true, helper: 'Installed memory.' },
  { key: 'storageGB', label: 'Minimum storage (GB)', numeric: true, helper: 'Total disk capacity.' },
  { key: 'gpuModel', label: 'GPU model', numeric: false, helper: 'Matched within the compute GPU model name, for example MI300X.' },
  { key: 'gpuCount', label: 'Minimum compute GPUs', numeric: true, helper: 'Compute GPUs, not display controllers.' },
]

const EMPTY_MINIMUM: MinimumHardware = {
  cpuCores: null,
  memoryMiB: null,
  storageGB: null,
  gpuModel: null,
  gpuCount: null,
}

function initialDraft(target: DeploymentTemplateEditorTarget, catalogs: readonly ProvisionerImageCatalog[]): TemplateDraft {
  if (target.kind === 'edit') {
    const { template } = target
    return {
      integrationId: template.integrationId,
      poolId: template.poolId ?? '',
      name: template.name,
      description: template.description,
      imageId: template.imageId,
      ephemeral: template.ephemeral,
      networkMode: template.network?.mode ?? 'automatic',
      subnetId: template.network?.subnetId ?? '',
      defaultGateway: template.network?.defaultGateway ?? false,
      minimumEnabled: Boolean(template.minimumHardware && !minimumHardwareIsEmpty(template.minimumHardware)),
      minimum: minimumHardwareDraft(template.minimumHardware),
      userDataEnabled: false,
      userData: '',
    }
  }
  const image = catalogs
    .find((catalog) => catalog.integrationId === target.integrationId)
    ?.images.find((candidate) => candidate.id === target.imageId)
  return {
    integrationId: target.integrationId,
    poolId: '',
    name: '',
    description: '',
    imageId: target.imageId,
    ephemeral: defaultDeployTargetForImage(image) === 'ram',
    networkMode: 'automatic',
    subnetId: '',
    defaultGateway: false,
    minimumEnabled: false,
    minimum: EMPTY_MINIMUM_HARDWARE_DRAFT,
    userDataEnabled: false,
    userData: '',
  }
}

function draftNetwork(draft: TemplateDraft): DeploymentNetworkSettings {
  return {
    mode: draft.networkMode,
    subnetId: draft.networkMode === 'static' ? draft.subnetId.trim() || undefined : undefined,
    defaultGateway: draft.networkMode === 'static' && draft.defaultGateway,
  }
}

function sameValue(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

/** Builds a sparse PATCH so an unrelated rename never revalidates an unchanged provider fact. */
function templateUpdate(
  template: DeploymentTemplate,
  draft: TemplateDraft,
  minimum: MinimumHardware,
  admin: boolean,
): UpdateDeploymentTemplateInput {
  const update: UpdateDeploymentTemplateInput = {}
  const nextNetwork = draftNetwork(draft)
  const currentNetwork: DeploymentNetworkSettings = {
    mode: template.network?.mode ?? 'automatic',
    subnetId: template.network?.mode === 'static' ? template.network.subnetId : undefined,
    defaultGateway: template.network?.mode === 'static' && (template.network.defaultGateway ?? false),
  }
  const currentMinimum = template.minimumHardware ?? EMPTY_MINIMUM
  const nextMinimum = draft.minimumEnabled ? minimum : EMPTY_MINIMUM
  if (draft.name.trim() !== template.name) update.name = draft.name.trim()
  if (draft.description.trim() !== template.description) update.description = draft.description.trim()
  if (draft.imageId !== template.imageId) update.imageId = draft.imageId
  if (draft.ephemeral !== template.ephemeral) update.deployTarget = deployTargetForEphemeral(draft.ephemeral)
  if (!sameValue(nextNetwork, currentNetwork)) update.network = nextNetwork
  if (!sameValue(nextMinimum, currentMinimum)) update.minimumHardware = nextMinimum
  if (admin && draft.poolId !== (template.poolId ?? '')) update.poolId = draft.poolId
  return update
}

/**
 * Two-step editor for the catalog's create and update flows.
 *
 * Site-wide image catalogs are supplied by the page, so opening the dialog performs no additional
 * catalog request. Cloud-init follows its write-only API boundary: create may include a new value,
 * while edit exposes independent Replace and Remove dialogs and never renders stored content.
 */
export function DeploymentTemplateDialog({
  target,
  catalogs,
  pools,
  currentUser,
  onClose,
  onChanged,
}: DeploymentTemplateDialogProps) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [step, setStep] = useState<1 | 2>(1)
  const [draft, setDraft] = useState(() => initialDraft(target, catalogs))
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)
  const [hasUserData, setHasUserData] = useState(target.kind === 'edit' && target.template.hasUserData)
  const [secretAction, setSecretAction] = useState<'replace' | 'remove' | null>(null)
  const [secretValue, setSecretValue] = useState('')
  const [secretBusy, setSecretBusy] = useState(false)
  const [secretFailure, setSecretFailure] = useState<DisplayedFailure | null>(null)
  const admin = isAdmin(currentUser)
  const editing = target.kind === 'edit'
  const selectedCatalog = catalogs.find((catalog) => catalog.integrationId === draft.integrationId)
  const images = selectedCatalog?.images ?? []
  const catalogUnavailable = Boolean(draft.integrationId && (!selectedCatalog || selectedCatalog.error))
  const permittedPools = useMemo(
    () => pools.filter((pool) => admin || poolRoleOf(currentUser, pool.id) === 'owner'),
    [admin, currentUser, pools],
  )

  // A Pool owner with one choice should not have to repeat information already implied by access.
  useEffect(() => {
    if (editing || draft.poolId || permittedPools.length !== 1) return
    const onlyPool = permittedPools.at(0)
    if (onlyPool) setDraft((current) => ({ ...current, poolId: onlyPool.id }))
  }, [draft.poolId, editing, permittedPools])

  const parsedMinimum = parseMinimumHardware(draft.minimum)
  const minimumError = draft.minimumEnabled && 'error' in parsedMinimum ? parsedMinimum.error : ''
  const stepOneValid = Boolean(
    draft.name.trim() &&
    draft.integrationId &&
    draft.imageId &&
    (editing || draft.poolId) &&
    (editing || !catalogUnavailable),
  )
  const stepTwoValid = Boolean(
    !minimumError &&
    (draft.networkMode !== 'static' || draft.subnetId.trim()) &&
    (!draft.userDataEnabled || draft.userData.trim()),
  )

  const handleIntegration = (integrationId: string) => {
    setDraft((current) => ({
      ...current,
      integrationId,
      imageId: '',
      ephemeral: true,
    }))
    setFailure(null)
  }

  const runPrimaryAction = async () => {
    if (step === 1) {
      if (stepOneValid) {
        setFailure(null)
        setStep(2)
      }
      return
    }
    if (busy || !stepTwoValid || !('minimum' in parsedMinimum)) return
    setBusy(true)
    setFailure(null)
    try {
      if (target.kind === 'create') {
        const minimum = draft.minimumEnabled ? parsedMinimum.minimum : EMPTY_MINIMUM
        await provisioning.createTemplate({
          integrationId: draft.integrationId,
          poolId: draft.poolId,
          name: draft.name.trim(),
          description: draft.description.trim() || undefined,
          imageId: draft.imageId,
          deployTarget: deployTargetForEphemeral(draft.ephemeral),
          network: draftNetwork(draft),
          ...(draft.minimumEnabled && !minimumHardwareIsEmpty(minimum) ? { minimumHardware: minimum } : {}),
          ...(draft.userDataEnabled ? { userData: draft.userData } : {}),
        })
        showToast({ tone: 'success', title: 'Deployment template created' })
      } else {
        const minimum = draft.minimumEnabled ? parsedMinimum.minimum : EMPTY_MINIMUM
        const update = templateUpdate(target.template, draft, minimum, admin)
        if (Object.keys(update).length > 0) {
          await provisioning.updateTemplate(target.template.id, update)
          showToast({ tone: 'success', title: 'Deployment template updated' })
        }
      }
      await onChanged()
      onClose()
    } catch (caught) {
      setFailure(failureOf(caught, 'The deployment template could not be saved.'))
    } finally {
      setBusy(false)
    }
  }

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    void runPrimaryAction()
  }

  const replaceSecret = async () => {
    if (target.kind !== 'edit' || !secretValue.trim() || secretBusy) return
    setSecretBusy(true)
    setSecretFailure(null)
    try {
      await provisioning.replaceTemplateUserData(target.template.id, secretValue)
      setHasUserData(true)
      setSecretValue('')
      setSecretAction(null)
      showToast({ tone: 'success', title: 'Cloud-init replaced' })
      await onChanged()
    } catch (caught) {
      setSecretFailure(failureOf(caught, 'Cloud-init could not be replaced.'))
    } finally {
      setSecretBusy(false)
    }
  }

  const removeSecret = async () => {
    if (target.kind !== 'edit' || secretBusy) return
    setSecretBusy(true)
    setSecretFailure(null)
    try {
      await provisioning.clearTemplateUserData(target.template.id)
      setHasUserData(false)
      setSecretAction(null)
      showToast({ tone: 'success', title: 'Cloud-init removed' })
      await onChanged()
    } catch (caught) {
      setSecretFailure(failureOf(caught, 'Cloud-init could not be removed.'))
    } finally {
      setSecretBusy(false)
    }
  }

  const poolOptions = editing && !admin
    ? pools.filter((pool) => pool.id === draft.poolId)
    : permittedPools
  const title = editing ? `Edit ${target.template.name}` : 'Create deployment template'

  return (
    <>
      <Modal
        open
        onClose={onClose}
        title={title}
        description="Save a reusable OS image, deploy mode, and network intent for one Pool."
        size="xl"
        contentClassName="sw-template-dialog"
        dismissDisabled={busy}
        onSubmit={(event) => void handleSubmit(event)}
        footer={(
          <div className="sw-template-dialog__footer">
            <Button type="button" variant="ghost" onClick={step === 1 ? onClose : () => setStep(1)} disabled={busy}>
              {step === 2 && <ArrowLeft size={15} aria-hidden />}
              {step === 1 ? 'Cancel' : 'Back'}
            </Button>
            <Button
              type="submit"
              colorPalette="brand"
              loading={busy}
              disabled={busy || (step === 1 ? !stepOneValid : !stepTwoValid)}
              onClick={(event) => {
                // Some custom picker controls retain native `required` inputs for semantics. Their
                // browser validation UI must not intercept the wizard's own step validation.
                event.preventDefault()
                void runPrimaryAction()
              }}
            >
              {step === 1 ? 'Continue' : editing ? 'Save changes' : 'Create template'}
              {step === 1 && <ArrowRight size={15} aria-hidden />}
            </Button>
          </div>
        )}
      >
        <ol className="sw-template-dialog__steps" aria-label="Template editor progress">
          <li aria-current={step === 1 ? 'step' : undefined} data-state={step === 1 ? 'current' : 'complete'}>
            <span>1</span><Text as="strong">Template &amp; image</Text>
          </li>
          <li aria-current={step === 2 ? 'step' : undefined} data-state={step === 2 ? 'current' : 'upcoming'}>
            <span>2</span><Text as="strong">Deployment settings</Text>
          </li>
        </ol>

        {failure && <Alert status="error" title="Could not save template"><FailureMessage failure={failure} /></Alert>}

        {step === 1 ? (
          <Stack gap="5">
            <div className="sw-template-dialog__grid">
              <Field.Root required>
                <Field.Label>Name <Field.RequiredIndicator /></Field.Label>
                <Input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} autoFocus />
              </Field.Root>
              <Field.Root>
                <Field.Label>Description</Field.Label>
                <Input value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} />
              </Field.Root>
              <Field.Root required>
                <Field.Label>Pool <Field.RequiredIndicator /></Field.Label>
                <Select
                  value={draft.poolId}
                  disabled={editing && !admin}
                  aria-label="Pool"
                  placeholder={pools.length === 0 ? 'Pool directory unavailable' : 'Select a Pool'}
                  onChange={(poolId) => setDraft((current) => ({ ...current, poolId }))}
                  options={poolOptions.map((pool) => ({ value: pool.id, label: pool.name }))}
                />
                <Field.HelperText>{editing && !admin ? 'Only an admin moves a Template to another Pool.' : 'Members of this Pool can use the Template; owners can change it.'}</Field.HelperText>
              </Field.Root>
              <Field.Root required>
                <Field.Label>Provisioner Integration <Field.RequiredIndicator /></Field.Label>
                {editing ? (
                  <Input value={selectedCatalog?.integrationName ?? draft.integrationId} readOnly aria-label="Provisioner Integration" />
                ) : (
                  <Select
                    value={draft.integrationId}
                    aria-label="Provisioner Integration"
                    placeholder="Select an Integration"
                    onChange={handleIntegration}
                    options={catalogs.map((catalog) => ({ value: catalog.integrationId, label: catalog.integrationName }))}
                  />
                )}
              </Field.Root>
            </div>

            {catalogUnavailable && (
              <Alert status="warning" title="Image catalog unavailable">
                {selectedCatalog?.error || 'The selected provisioner is not available in the current Site scope.'}
                {editing && ' You can keep the existing image and change unrelated settings.'}
              </Alert>
            )}
            <OSImagePicker
              images={images}
              value={draft.imageId}
              siteId={selectedCatalog?.siteId}
              integrationId={draft.integrationId}
              required
              disabled={!draft.integrationId || catalogUnavailable}
              onChange={(imageId) => {
                const image = images.find((candidate) => candidate.id === imageId)
                setDraft((current) => ({
                  ...current,
                  imageId,
                  ephemeral: defaultDeployTargetForImage(image) === 'ram',
                }))
              }}
            />
          </Stack>
        ) : (
          <Stack gap="6">
            <section className="sw-template-dialog__section" aria-labelledby="template-installation-heading">
              <Heading id="template-installation-heading" size="sm">Installation</Heading>
              <Text color="fg.muted" fontSize="sm">Choose whether the Server boots from disk or runs the image from memory.</Text>
              <DeployTargetField
                value={deployTargetForEphemeral(draft.ephemeral)}
                onChange={(targetValue) => setDraft((current) => ({ ...current, ephemeral: deployTargetIsEphemeral(targetValue) }))}
              />
            </section>

            <section className="sw-template-dialog__section" aria-labelledby="template-network-heading">
              <Heading id="template-network-heading" size="sm">Networking</Heading>
              <Text color="fg.muted" fontSize="sm">Store reusable addressing intent; target-specific NICs and static addresses are selected at deployment time.</Text>
              <AddressingModeField
                label="Network mode"
                value={draft.networkMode}
                onChange={(networkMode) => setDraft((current) => ({
                  ...current,
                  networkMode,
                  defaultGateway: networkMode === 'static' ? current.defaultGateway : false,
                }))}
              />
              {draft.networkMode === 'static' && (
                <div className="sw-template-dialog__grid">
                  <Field.Root required>
                    <Field.Label>Subnet ID <Field.RequiredIndicator /></Field.Label>
                    <Input value={draft.subnetId} onChange={(event) => setDraft((current) => ({ ...current, subnetId: event.target.value }))} />
                  </Field.Root>
                  <Field.Root alignSelf="end">
                    <Checkbox checked={draft.defaultGateway} onCheckedChange={(checked) => setDraft((current) => ({ ...current, defaultGateway: checked }))}>
                      Use as default gateway
                    </Checkbox>
                  </Field.Root>
                </div>
              )}
            </section>

            <section className="sw-template-dialog__option" aria-labelledby="template-hardware-heading">
              <div className="sw-template-dialog__option-header">
                <div>
                  <Heading id="template-hardware-heading" size="sm">Minimum hardware</Heading>
                  <Text color="fg.muted" fontSize="sm">Optional eligibility floor for new Platform node-group members. Single-Server deployments ignore it.</Text>
                </div>
                <Switch.Root checked={draft.minimumEnabled} onCheckedChange={(details) => setDraft((current) => ({ ...current, minimumEnabled: details.checked === true }))}>
                  <Switch.HiddenInput aria-label="Include minimum hardware" />
                  <Switch.Control><Switch.Thumb /></Switch.Control>
                  <Text as="span" aria-hidden="true" fontSize="sm">{draft.minimumEnabled ? 'Included' : 'Not included'}</Text>
                </Switch.Root>
              </div>
              {draft.minimumEnabled && (
                <div className="sw-template-dialog__minimum-grid">
                  {MINIMUM_FIELDS.map((field) => (
                    <Field.Root key={field.key}>
                      <Field.Label>{field.label}</Field.Label>
                      <Input
                        value={draft.minimum[field.key]}
                        inputMode={field.numeric ? 'decimal' : 'text'}
                        onChange={(event) => setDraft((current) => ({ ...current, minimum: { ...current.minimum, [field.key]: event.target.value } }))}
                      />
                      <Field.HelperText>{field.helper}</Field.HelperText>
                    </Field.Root>
                  ))}
                </div>
              )}
              {minimumError && <Text color="fg.error" fontSize="sm" role="alert">{minimumError}</Text>}
            </section>

            <section className="sw-template-dialog__option" aria-labelledby="template-cloud-init-heading">
              <div className="sw-template-dialog__option-header">
                <div>
                  <Heading id="template-cloud-init-heading" size="sm"><Cloud size={16} aria-hidden /> Cloud-init</Heading>
                  <Text color="fg.muted" fontSize="sm">Cloud-init is encrypted and write-only. Stored content can only be replaced or removed.</Text>
                </div>
                {editing ? (
                  <BadgeState configured={hasUserData} />
                ) : (
                  <Switch.Root checked={draft.userDataEnabled} onCheckedChange={(details) => setDraft((current) => ({ ...current, userDataEnabled: details.checked === true }))}>
                    <Switch.HiddenInput aria-label="Include cloud-init" />
                    <Switch.Control><Switch.Thumb /></Switch.Control>
                    <Text as="span" aria-hidden="true" fontSize="sm">{draft.userDataEnabled ? 'Included' : 'Not included'}</Text>
                  </Switch.Root>
                )}
              </div>
              {!editing && draft.userDataEnabled && (
                <Field.Root required>
                  <Field.Label>Cloud-init <Field.RequiredIndicator /></Field.Label>
                  <Textarea value={draft.userData} onChange={(event) => setDraft((current) => ({ ...current, userData: event.target.value }))} rows={8} autoComplete="off" />
                </Field.Root>
              )}
              {editing && (
                <Stack direction={{ base: 'column', sm: 'row' }} gap="2" alignItems="flex-start">
                  <Button type="button" size="sm" variant="outline" onClick={() => { setSecretFailure(null); setSecretAction('replace') }}>
                    {hasUserData ? 'Replace cloud-init' : 'Add cloud-init'}
                  </Button>
                  {hasUserData && <Button type="button" size="sm" variant="ghost" colorPalette="red" onClick={() => { setSecretFailure(null); setSecretAction('remove') }}>Remove cloud-init</Button>}
                </Stack>
              )}
            </section>
          </Stack>
        )}
      </Modal>

      {target.kind === 'edit' && (
        <>
          <Modal
            open={secretAction === 'replace'}
            onClose={() => { setSecretAction(null); setSecretValue(''); setSecretFailure(null) }}
            title={`${hasUserData ? 'Replace' : 'Add'} cloud-init`}
            description={`The existing value for ${target.template.name} cannot be displayed.`}
            dismissDisabled={secretBusy}
            footer={(
              <>
                <Button variant="ghost" onClick={() => { setSecretAction(null); setSecretValue(''); setSecretFailure(null) }} disabled={secretBusy}>Cancel</Button>
                <Button colorPalette="brand" loading={secretBusy} disabled={!secretValue.trim() || secretBusy} onClick={() => void replaceSecret()}>
                  {hasUserData ? 'Replace cloud-init' : 'Add cloud-init'}
                </Button>
              </>
            )}
          >
            <Stack gap="4">
              {secretFailure && <Alert status="error" title="Could not replace cloud-init"><FailureMessage failure={secretFailure} /></Alert>}
              <Field.Root required>
                <Field.Label>New cloud-init <Field.RequiredIndicator /></Field.Label>
                <Textarea value={secretValue} onChange={(event) => setSecretValue(event.target.value)} rows={10} autoComplete="off" autoFocus />
              </Field.Root>
            </Stack>
          </Modal>
          <Modal
            open={secretAction === 'remove'}
            onClose={() => { setSecretAction(null); setSecretFailure(null) }}
            title="Remove cloud-init?"
            description={`New deployments using ${target.template.name} will no longer inherit its cloud-init.`}
            role="alertdialog"
            dismissDisabled={secretBusy}
            footer={(
              <>
                <Button variant="ghost" onClick={() => { setSecretAction(null); setSecretFailure(null) }} disabled={secretBusy}>Cancel</Button>
                <Button colorPalette="red" loading={secretBusy} disabled={secretBusy} onClick={() => void removeSecret()}>Remove cloud-init</Button>
              </>
            )}
          >
            {secretFailure && <Alert status="error" title="Could not remove cloud-init"><FailureMessage failure={secretFailure} /></Alert>}
          </Modal>
        </>
      )}
    </>
  )
}

/** Text status used instead of reading back the secret cloud-init value. */
function BadgeState({ configured }: { configured: boolean }) {
  return <Text fontSize="sm" fontWeight="semibold">{configured ? 'Configured' : 'Not configured'}</Text>
}
