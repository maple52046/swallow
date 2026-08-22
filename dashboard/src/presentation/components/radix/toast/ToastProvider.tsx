import { useCallback, useMemo, useRef, useState } from 'react'
import * as Toast from '@radix-ui/react-toast'
import { Callout, Flex, Text } from '@radix-ui/themes'
import {
  CheckCircledIcon,
  CrossCircledIcon,
  ExclamationTriangleIcon,
  InfoCircledIcon,
} from '@radix-ui/react-icons'
import { ToastContext, type ToastOptions, type ToastTone } from './toastContext'

/** An enqueued toast plus the render-time state Radix needs to animate it out. */
interface ActiveToast extends ToastOptions {
  id: number
  open: boolean
}

/** Radix Themes accent colour per tone. */
const TONE_COLOR: Record<ToastTone, 'blue' | 'green' | 'amber' | 'red'> = {
  info: 'blue',
  success: 'green',
  warning: 'amber',
  error: 'red',
}

/** Default auto-dismiss per tone; errors linger because they usually need a decision. */
const TONE_DURATION: Record<ToastTone, number> = {
  info: 4000,
  success: 4000,
  warning: 6000,
  error: 8000,
}

function ToneIcon({ tone }: { tone: ToastTone }) {
  switch (tone) {
    case 'success':
      return <CheckCircledIcon />
    case 'warning':
      return <ExclamationTriangleIcon />
    case 'error':
      return <CrossCircledIcon />
    default:
      return <InfoCircledIcon />
  }
}

interface ToastProviderProps {
  children: React.ReactNode
}

/**
 * App-wide toast host. Provides `useToast` and renders queued messages in a fixed
 * viewport at the top-right.
 *
 * Messages are held in state and removed after they animate closed, so the DOM does not
 * accumulate dismissed toasts. Tone is conveyed by both an icon and colour, never colour
 * alone, to satisfy the accessibility gate.
 */
export function ToastProvider({ children }: ToastProviderProps) {
  const [toasts, setToasts] = useState<ActiveToast[]>([])
  // Monotonic id source; a ref so re-renders never reuse an id and break React keys.
  const nextId = useRef(0)

  const showToast = useCallback((options: ToastOptions) => {
    const id = nextId.current++
    setToasts((current) => [...current, { ...options, id, open: true }])
  }, [])

  // Flip open=false first so Radix plays the close animation, then drop it from state.
  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.map((toast) => (toast.id === id ? { ...toast, open: false } : toast)))
  }, [])

  const remove = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])

  const value = useMemo(() => ({ showToast }), [showToast])

  return (
    <ToastContext.Provider value={value}>
      <Toast.Provider swipeDirection="right">
        {children}

        {toasts.map((toast) => {
          const tone = toast.tone ?? 'info'
          return (
            <Toast.Root
              key={toast.id}
              open={toast.open}
              duration={toast.duration ?? TONE_DURATION[tone]}
              onOpenChange={(open) => {
                if (!open) dismiss(toast.id)
              }}
              // Radix fires this after the exit animation; only then is it safe to unmount.
              onAnimationEnd={() => {
                if (!toast.open) remove(toast.id)
              }}
              asChild
            >
              <Callout.Root color={TONE_COLOR[tone]} role="status" style={{ minWidth: 280 }}>
                <Flex gap="2" align="start">
                  <Callout.Icon>
                    <ToneIcon tone={tone} />
                  </Callout.Icon>
                  <Flex direction="column" gap="1">
                    <Toast.Title asChild>
                      <Text size="2" weight="medium">
                        {toast.title}
                      </Text>
                    </Toast.Title>
                    {toast.description && (
                      <Toast.Description asChild>
                        <Text size="1" color="gray">
                          {toast.description}
                        </Text>
                      </Toast.Description>
                    )}
                  </Flex>
                </Flex>
              </Callout.Root>
            </Toast.Root>
          )
        })}

        <Toast.Viewport
          style={{
            position: 'fixed',
            top: 12,
            right: 12,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
            width: 360,
            maxWidth: '100vw',
            margin: 0,
            padding: 0,
            listStyle: 'none',
            zIndex: 9999,
            outline: 'none',
          }}
        />
      </Toast.Provider>
    </ToastContext.Provider>
  )
}
