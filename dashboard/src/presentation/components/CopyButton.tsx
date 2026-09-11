import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react'
import { IconButton } from '@chakra-ui/react'
import { Check, Copy } from 'lucide-react'
import { copyText } from '@/presentation/utils/clipboard'
import { Tooltip } from '@/presentation/components/ui/tooltip'

interface CopyButtonProps {
  /** The exact text placed on the clipboard (e.g. a hostname, MAC, or IP address). */
  value: string
  /**
   * Accessible name and idle tooltip, phrased as an action — for example "Copy
   * hostname". It is the only cue a screen-reader user gets, so it must name the field.
   */
  label: string
}

/**
 * Inline, icon-only button that copies a single field value to the clipboard.
 *
 * Rendered next to a displayed identifier (hostname, MAC, IP) in dense tables and
 * detail views so an operator can grab the value without selecting text. This is
 * the shared source for inline value copying — compose it rather than
 * re-implementing `navigator.clipboard`.
 *
 * Contracts:
 * - Renders nothing when `value` is empty, so callers may pass an optional field
 *   directly without guarding.
 * - Stops click propagation so it is safe inside a clickable table row.
 * - Confirms success by swapping to a transient green check + "Copied" tooltip
 *   (status by icon + colour + text, never colour alone); the state auto-resets
 *   and is cleaned up on unmount.
 * - Treats clipboard access as best-effort: a denied permission or insecure origin
 *   fails quietly and leaves the idle icon.
 */
export function CopyButton({ value, label }: CopyButtonProps) {
  const [copied, setCopied] = useState(false)
  // Hold the reset timer so a rapid second copy, or an unmount, cannot fire setState
  // against a gone component.
  const resetTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => {
    if (resetTimer.current) clearTimeout(resetTimer.current)
  }, [])

  const copy = useCallback(
    async (event: MouseEvent<HTMLButtonElement>) => {
      event.stopPropagation()
      // copyText handles insecure origins (LAN HTTP) via a fallback; only confirm on success.
      const ok = await copyText(value)
      if (!ok) return
      setCopied(true)
      if (resetTimer.current) clearTimeout(resetTimer.current)
      resetTimer.current = setTimeout(() => setCopied(false), 2000)
    },
    [value],
  )

  if (!value) return null
  return (
    <Tooltip content={copied ? 'Copied' : label}>
      <IconButton
        variant="ghost"
        size="2xs"
        aria-label={label}
        color={copied ? 'green.fg' : 'fg.muted'}
        onClick={copy}
      >
        {copied ? <Check size={14} /> : <Copy size={14} />}
      </IconButton>
    </Tooltip>
  )
}
