import { createContext, useContext } from 'react'

/** Semantic notification tone; PatternFly pairs every tone with icon and text. */
export type ToastTone = 'info' | 'success' | 'warning' | 'error'

/** A transient operator message. Duration is milliseconds and may override tone defaults. */
export interface ToastOptions {
  title: string
  description?: string
  tone?: ToastTone
  duration?: number
}

/** Notification channel exposed to async actions without coupling them to Alert markup. */
export interface ToastContextValue {
  showToast: (options: ToastOptions) => void
}

export const ToastContext = createContext<ToastContextValue | null>(null)
/** Nullable provider context; consumers should use {@link useToast} for checked access. */

/** Returns the global toast channel; missing wiring is treated as an application error. */
export function useToast(): ToastContextValue {
  const value = useContext(ToastContext)
  if (value === null) throw new Error('useToast must be used within ToastProvider')
  return value
}
