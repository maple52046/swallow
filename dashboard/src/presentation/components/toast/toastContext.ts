import { toaster } from '@/presentation/components/ui/toaster'

/** Semantic notification tone; each tone pairs a Chakra indicator with text. */
export type ToastTone = 'info' | 'success' | 'warning' | 'error'

/** A transient operator message. Duration is milliseconds and may override tone defaults. */
export interface ToastOptions {
  title: string
  description?: string
  tone?: ToastTone
  duration?: number
}

/** Notification channel exposed to async actions without coupling them to toast markup. */
export interface ToastContextValue {
  showToast: (options: ToastOptions) => void
}

// Chakra's toast type names differ from the console's tone vocabulary; `error`
// maps to the danger indicator. Kept next to the tone type so both evolve together.
const TONE_TO_TYPE: Record<ToastTone, 'info' | 'success' | 'warning' | 'error'> = {
  info: 'info',
  success: 'success',
  warning: 'warning',
  error: 'error',
}

// Per-tone dwell time: failures linger longest so operators can read the cause,
// routine confirmations clear quickly. Overridable per call via `duration`.
const DURATION: Record<ToastTone, number> = {
  info: 4000,
  success: 4000,
  warning: 6000,
  error: 8000,
}

/**
 * Returns the global toast channel.
 *
 * Backed by the module-level Chakra toaster store, so no provider wiring is
 * required and the returned `showToast` is stable across renders. The `<Toaster />`
 * host (mounted at the app root) renders whatever is queued here.
 */
export function useToast(): ToastContextValue {
  return { showToast }
}

/** Queues a toast; separated from the hook so non-component code can notify too. */
function showToast(options: ToastOptions): void {
  const tone = options.tone ?? 'info'
  toaster.create({
    title: options.title,
    description: options.description,
    type: TONE_TO_TYPE[tone],
    duration: options.duration ?? DURATION[tone],
    closable: true,
  })
}
