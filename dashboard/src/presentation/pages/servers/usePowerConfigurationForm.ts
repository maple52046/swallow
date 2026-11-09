import { useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  serverDisplayName,
  type PowerDriverOption,
  type PowerFamily,
  type Server,
  type ServerPowerConfiguration,
} from '@/domain/server/types'
import { powerStateLabel } from '@/presentation/components/axisBadgeUtils'
import { powerDriverLabel } from '@/presentation/components/serverSummary/powerConfigurationLabels'
import { useToast } from '@/presentation/components/toast/toastContext'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import {
  draftFamily,
  powerConfigurationDraftErrors,
  powerConfigurationInput,
  type PowerConfigurationDraft,
} from './serverPowerConfigurationPresentation'

/**
 * One edit of a Server's Power Configuration (decision 054), as `usePowerConfigurationForm`
 * returns it for `PowerConfigurationFields` and the dialog that hosts them.
 */
export interface PowerConfigurationForm {
  /** The drivers a write may choose, from the configuration being edited; empty before it is read. */
  readonly options: readonly PowerDriverOption[]
  /** The host has not read the configuration yet: the driver choice and saving wait for it. */
  readonly reading: boolean
  readonly draft: PowerConfigurationDraft
  /** The chosen driver's family, which decides the fields; null before a choice. */
  readonly family: PowerFamily | null
  readonly errors: ReturnType<typeof powerConfigurationDraftErrors>
  /** Whether a save was attempted, so field errors show only after it. */
  readonly touched: boolean
  readonly busy: boolean
  /** The API's refusal of the last save (its own or the provisioner's). */
  readonly error: DisplayedFailure | null
  /** A locked Server cannot be changed; the fields say why and saving is disabled. */
  readonly locked: boolean
  readonly canClearPassword: boolean
  readonly passwordHelper: string
  /** Whether the host's Save button is enabled. */
  readonly canSubmit: boolean
  update(patch: Partial<PowerConfigurationDraft>): void
  /** Writes the draft through to the provisioner; resolves when it finished, whether it succeeded or not. */
  save(): Promise<void>
}

/** The draft an edit of configuration starts from. Without a driver nothing is pre-selected. */
function initialDraft(configuration: ServerPowerConfiguration | null): PowerConfigurationDraft {
  const driver = configuration?.drivers.some((option) => option.driver === configuration.driver) ? configuration.driver : ''
  return {
    driver,
    address: configuration?.address ?? '',
    powerId: configuration?.powerId ?? '',
    username: configuration?.username ?? '',
    password: '',
    clearPassword: false,
  }
}

/**
 * Holds one Power Configuration edit for both places that offer it: the Server Summary's dialog and
 * the Server list's power dialog, so they validate, save, and explain the password the same way.
 *
 * `configuration` is the provisioner's configuration as read live, or null while the host is still
 * reading it (the Server list's dialog can open the form before the read returns); until then
 * `reading` is true, the driver choice is disabled, and nothing can be saved, so no typing is lost
 * when the read arrives and restarts the edit. Each newly read
 * configuration — the first read, or the one a save returned — starts a new edit from its driver
 * and parameters; without a driver nothing is pre-selected, because the dashboard cannot tell a BMC
 * from a virtual machine. Hosts must therefore pass the same object for as long as one edit lasts.
 *
 * The password is write-only: it lives only in this hook's state, is never shown back, and is
 * dropped when the host unmounts; leaving it empty keeps the stored one for an unchanged driver.
 * `save` writes through `ServerRepository.setPowerConfiguration`, which keeps the configuration only
 * when the provisioner reads the Server's power through it (server-detail-actions.md); the call can
 * take up to the API's 20-second check, and `busy` covers all of it. On success
 * it shows a toast with the power the check read, refreshes the Server's projection, and calls
 * `onSaved` with the configuration read back; on failure (a wrong address or password included,
 * the previous configuration then still in place) it keeps the draft, password too, so the user
 * can correct it, and exposes the refusal as `error`. Saving never switches power or resumes an
 * inspection.
 */
export function usePowerConfigurationForm(
  server: Server,
  configuration: ServerPowerConfiguration | null,
  onSaved: (configuration: ServerPowerConfiguration) => void,
): PowerConfigurationForm {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [source, setSource] = useState(configuration)
  const [draft, setDraft] = useState(() => initialDraft(configuration))
  const [touched, setTouched] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  if (source !== configuration) {
    // Adjusting state while rendering (rather than in an effect) restarts the edit before the
    // stale draft is ever shown for the new configuration.
    setSource(configuration)
    setDraft(initialDraft(configuration))
    setTouched(false)
    setError(null)
  }
  const options = configuration?.drivers ?? []
  const locked = server.status.provisioning?.locked ?? false
  const errors = powerConfigurationDraftErrors(draft, options)
  const valid = Object.keys(errors).length === 0
  const passwordSet = configuration?.passwordSet ?? false
  const canClearPassword = draft.driver === configuration?.driver && passwordSet

  const reading = configuration === null

  const save = async () => {
    if (reading) return
    setTouched(true)
    if (!valid || busy || locked) return
    setBusy(true)
    setError(null)
    try {
      const saved = await servers.setPowerConfiguration(server.id, powerConfigurationInput(draft, options))
      const read = saved.powerState ? ` and reads its power as ${powerStateLabel(saved.powerState).toLowerCase()}` : ''
      showToast({
        tone: 'success',
        title: 'Power configuration saved',
        description: `The provisioner now powers ${serverDisplayName(server)} through ${powerDriverLabel(saved.driver)}${read}. Retry its hardware inspection if it waits for attention.`,
      })
      // The Server's projection (its power state and power control) follows at once rather than
      // at the next inventory sync; best effort, because that sync does the same.
      void servers.refreshServer(server.id).catch(() => undefined)
      onSaved(saved)
    } catch (caught) {
      setError(failureOf(caught, 'The power configuration could not be saved.'))
    } finally {
      setBusy(false)
    }
  }

  const passwordHelper = canClearPassword
    ? 'Leave empty to keep the stored password. swallow sends it to the provisioner and does not store it.'
    : passwordSet
      ? 'Changing the driver removes the stored password unless you enter one. swallow does not store it.'
      : 'Optional. swallow sends it to the provisioner and does not store it.'

  return {
    options,
    reading,
    draft,
    family: draftFamily(draft, options),
    errors,
    touched,
    busy,
    error,
    locked,
    canClearPassword,
    passwordHelper,
    canSubmit: !reading && !busy && !locked && !(touched && !valid),
    update: (patch) => setDraft((current) => ({ ...current, ...patch })),
    save,
  }
}
