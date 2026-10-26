import { Settings2 } from 'lucide-react'
import type { ReactNode } from 'react'

interface AdvancedSettingsProps {
  children: ReactNode
  label?: string
  open?: boolean
}
/**
 * Native disclosure for low-frequency deployment controls. Using `details` preserves browser
 * keyboard and assistive-technology behavior without coupling form state to presentation state.
 */
export function AdvancedSettings({ children, label = 'Advanced settings', open = false }: AdvancedSettingsProps) {
  return (
    <details className="sw-platform-deploy-advanced" open={open}>
      <summary><Settings2 size={15} aria-hidden="true" />{label}</summary>
      <div className="sw-platform-deploy-advanced__content">{children}</div>
    </details>
  )
}
