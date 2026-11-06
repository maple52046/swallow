import { useRef, type ReactNode } from 'react'
import { Button } from '@chakra-ui/react'
import { Modal } from '@/presentation/components/ui/modal'

interface ConfirmDialogProps {
  open: boolean
  title: string
  /** Names the action and, ideally, its target ("Delete namespace", "Remove image"). */
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  /** Disables both buttons and shows progress on confirm while the action runs. */
  busy?: boolean
  /**
   * Whether the action destroys or removes something (the default), which the red confirm button
   * signals. A consequential but non-destructive change, such as switching a setting, passes
   * `false` for the console's primary colour.
   */
  destructive?: boolean
  /** The consequence statement: what is affected and whether it can be undone. */
  children: ReactNode
}

/**
 * Confirmation dialog for a destructive or irreversible action: explorer deletions (Kubernetes
 * namespaces, Docker images, containers, volumes, networks), Boot ISO rebuild and delete, Change
 * Plan approval and rejection, and discarding an unsaved draft, among others. With
 * `destructive={false}` it also confirms a consequential setting change, such as where a Site's
 * deployments fetch their artifacts.
 *
 * Rendered as an `alertdialog` so assistive tech announces it as requiring a decision; the body must
 * name the target and the consequence. It opens with focus on Cancel, the safe choice, so a stray
 * Enter or Space dismisses rather than confirms and keyboard users reach the confirm button only on
 * purpose. Cancel is first in the footer and dismissal is blocked while `busy`, so a slow action
 * cannot be abandoned mid-flight or confirmed twice. Renders nothing when closed so the caller can
 * keep it mounted.
 */
export function ConfirmDialog({ open, title, confirmLabel, onConfirm, onCancel, busy = false, destructive = true, children }: ConfirmDialogProps) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  if (!open) return null
  return (
    <Modal
      open
      onClose={() => !busy && onCancel()}
      closeOnInteractOutside={!busy}
      title={title}
      role="alertdialog"
      initialFocusEl={() => cancelRef.current}
      footer={
        <>
          <Button ref={cancelRef} variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button colorPalette={destructive ? 'red' : 'brand'} onClick={onConfirm} loading={busy} disabled={busy}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Modal>
  )
}
