import { useCallback, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  Alert,
  AlertActionCloseButton,
  AlertGroup,
  AlertVariant,
} from '@patternfly/react-core'
import { ToastContext, type ToastOptions, type ToastTone } from './toastContext'

interface ActiveToast extends ToastOptions {
  id: number
}

const VARIANT: Record<ToastTone, AlertVariant> = {
  info: AlertVariant.info,
  success: AlertVariant.success,
  warning: AlertVariant.warning,
  error: AlertVariant.danger,
}

const DURATION: Record<ToastTone, number> = {
  info: 4000,
  success: 4000,
  warning: 6000,
  error: 8000,
}

/**
 * PatternFly toast host for application-wide action feedback.
 *
 * AlertGroup supplies the live region and keyboard-accessible close controls. Messages
 * are removed on timeout or explicit dismissal so stale alerts do not accumulate.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ActiveToast[]>([])
  const nextId = useRef(0)

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])

  const showToast = useCallback((options: ToastOptions) => {
    const id = nextId.current++
    setToasts((current) => [...current, { ...options, id }])
  }, [])

  const value = useMemo(() => ({ showToast }), [showToast])
  return (
    <ToastContext.Provider value={value}>
      {children}
      <AlertGroup isToast isLiveRegion aria-label="Console notifications">
        {toasts.map((toast) => {
          const tone = toast.tone ?? 'info'
          return (
            <Alert
              key={toast.id}
              variant={VARIANT[tone]}
              title={toast.title}
              timeout={toast.duration ?? DURATION[tone]}
              onTimeout={() => dismiss(toast.id)}
              actionClose={
                <AlertActionCloseButton
                  title={toast.title}
                  onClose={() => dismiss(toast.id)}
                />
              }
            >
              {toast.description}
            </Alert>
          )
        })}
      </AlertGroup>
    </ToastContext.Provider>
  )
}
