import { chakra, CloseButton, Dialog, Portal } from '@chakra-ui/react'
import type { FormEvent, ReactNode } from 'react'
import { InsideDialogContext } from './dialog-portal-context'

interface ModalProps {
  /** Controlled open state; the dialog is fully controlled by the caller. */
  open: boolean
  /** Called on any dismissal (close button, backdrop, Escape) so the caller can reset state. */
  onClose: () => void
  title: ReactNode
  /**
   * Optional, non-redundant context announced as the dialog description and rendered on its
   * own line below the title. Omit it when the body already explains the action.
   */
  description?: ReactNode
  /** Dialog body content. */
  children: ReactNode
  /** Footer actions (typically a cancel + confirm pair). */
  footer?: ReactNode
  size?: Dialog.RootProps['size']
  /** Prevents backdrop/Escape dismissal for destructive flows that must be answered. */
  closeOnInteractOutside?: boolean
  role?: Dialog.RootProps['role']
  /** Element that receives focus when the dialog opens. */
  initialFocusEl?: Dialog.RootProps['initialFocusEl']
  /**
   * When provided, the header/body/footer are wrapped in a `<form>` so Enter submits
   * and the footer's `type="submit"` button triggers `onSubmit`.
   */
  onSubmit?: (event: FormEvent<HTMLFormElement>) => void
}

/**
 * Shared modal scaffold wrapping Chakra's `Dialog`.
 *
 * Every console dialog composes this rather than repeating the
 * backdrop/positioner/content/close plumbing, keeping focus trapping, Escape
 * handling, and the close affordance consistent. Pass `onSubmit` to turn it into a
 * form dialog (Enter submits); omit it for confirmation dialogs whose footer wires
 * its own handlers. It is fully controlled — mount it always and drive `open`.
 */
export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'md',
  closeOnInteractOutside = true,
  role,
  initialFocusEl,
  onSubmit,
}: ModalProps) {
  const inner = (
    <>
      <Dialog.Header flexDirection="column" alignItems="stretch" gap="1" pe="12">
        <Dialog.Title minW="0" width="full">{title}</Dialog.Title>
        {description && <Dialog.Description width="full" lineHeight="tall">{description}</Dialog.Description>}
      </Dialog.Header>
      <Dialog.Body minW="0">{children}</Dialog.Body>
      {footer && <Dialog.Footer flexWrap="wrap">{footer}</Dialog.Footer>}
    </>
  )

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(event) => {
        if (!event.open) onClose()
      }}
      size={size}
      placement="center"
      role={role}
      initialFocusEl={initialFocusEl}
      closeOnInteractOutside={closeOnInteractOutside}
    >
      <Portal>
        <Dialog.Backdrop />
        <Dialog.Positioner>
          <Dialog.Content>
            {/* Nested Selects read this flag and render their listbox inline instead of
                portalling to the body, which mis-positions inside a focus-trapped dialog. */}
            <InsideDialogContext.Provider value={true}>
              {onSubmit ? (
                <chakra.form onSubmit={onSubmit} display="contents">
                  {inner}
                </chakra.form>
              ) : (
                inner
              )}
            </InsideDialogContext.Provider>
            <Dialog.CloseTrigger asChild>
              <CloseButton size="sm" />
            </Dialog.CloseTrigger>
          </Dialog.Content>
        </Dialog.Positioner>
      </Portal>
    </Dialog.Root>
  )
}
