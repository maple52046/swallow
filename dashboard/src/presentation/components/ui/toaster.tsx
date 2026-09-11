import {
  Toaster as ChakraToaster,
  Portal,
  Spinner,
  Stack,
  Toast,
  createToaster,
} from '@chakra-ui/react'

/**
 * Global toast store for application-wide action feedback.
 *
 * Created once at module load. Feature code does not call this directly; it goes
 * through `useToast().showToast` (see `components/toast/toastContext`) so call
 * sites stay decoupled from Chakra's toast markup. The `<Toaster />` host must be
 * mounted once near the app root to render whatever is queued here.
 */
export const toaster = createToaster({
  placement: 'bottom-end',
  pauseOnPageIdle: true,
  gap: 12,
})

/**
 * Renders queued toasts in a live region.
 *
 * A single host lives at the composition root. Each toast pairs a semantic
 * indicator (or a spinner while loading) with title/description text, so status
 * is never conveyed by colour alone, and exposes a keyboard-accessible close
 * control.
 */
export function Toaster() {
  return (
    <Portal>
      <ChakraToaster toaster={toaster} insetInline={{ mdDown: '4' }}>
        {(toast) => (
          <Toast.Root width={{ md: 'sm' }}>
            {toast.type === 'loading' ? (
              <Spinner size="sm" color="brand.solid" />
            ) : (
              <Toast.Indicator />
            )}
            <Stack gap="1" flex="1" maxWidth="100%">
              {toast.title && <Toast.Title>{toast.title}</Toast.Title>}
              {toast.description && <Toast.Description>{toast.description}</Toast.Description>}
            </Stack>
            {toast.action && <Toast.ActionTrigger>{toast.action.label}</Toast.ActionTrigger>}
            {toast.closable && <Toast.CloseTrigger />}
          </Toast.Root>
        )}
      </ChakraToaster>
    </Portal>
  )
}
