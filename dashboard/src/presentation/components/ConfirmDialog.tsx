import type { ReactNode } from 'react'
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
  /** The consequence statement: what is affected and whether it can be undone. */
  children: ReactNode
}

/**
 * Confirmation dialog for a destructive explorer action (Kubernetes namespaces, Docker images,
 * containers, volumes, networks).
 *
 * Rendered as an `alertdialog` so assistive tech announces it as requiring a decision; the body must
 * name the target and the consequence. Cancel is first in the footer and dismissal is blocked while
 * `busy`, so a slow action cannot be abandoned mid-flight or confirmed twice. Renders nothing when
 * closed so the caller can keep it mounted.
 */
export function ConfirmDialog({ open, title, confirmLabel, onConfirm, onCancel, busy = false, children }: ConfirmDialogProps) {
  if (!open) return null
  return (
    <Modal
      open
      onClose={() => !busy && onCancel()}
      closeOnInteractOutside={!busy}
      title={title}
      role="alertdialog"
      footer={
        <>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button colorPalette="red" onClick={onConfirm} loading={busy} disabled={busy}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Modal>
  )
}
