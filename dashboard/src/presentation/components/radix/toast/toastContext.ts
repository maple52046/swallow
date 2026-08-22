/**
 * Toast context and hook, split from `ToastProvider` so the provider file exports only a
 * component (Fast Refresh requires that).
 *
 * This replaces `@mantine/notifications`: Radix Themes ships no toast, so the app-wide
 * transient-message channel is built on `@radix-ui/react-toast` and reached through
 * `useToast`.
 */
import { createContext, useContext } from 'react'

/** Semantic tone of a toast; drives its colour and icon, never colour alone. */
export type ToastTone = 'info' | 'success' | 'warning' | 'error'

/** A single transient message. `description` is optional detail under the title. */
export interface ToastOptions {
  title: string
  description?: string
  tone?: ToastTone
  /** Auto-dismiss delay in ms; defaults to a tone-appropriate value in the provider. */
  duration?: number
}

export interface ToastContextValue {
  /** Enqueues a toast. Safe to call from event handlers and async callbacks. */
  showToast: (options: ToastOptions) => void
}

export const ToastContext = createContext<ToastContextValue | null>(null)

/**
 * Returns the toast channel.
 *
 * Throws when used outside `ToastProvider`, because a dropped message is worse than a
 * loud wiring error during development.
 */
export function useToast(): ToastContextValue {
  const value = useContext(ToastContext)
  if (value === null) {
    throw new Error('useToast must be used within ToastProvider')
  }
  return value
}
