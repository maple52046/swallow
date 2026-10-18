import { useEffect, useState } from 'react'
import { SearchInput } from './search-input'

interface DebouncedSearchInputProps {
  /** The committed query the page owns, normally a URL search parameter. */
  value: string
  /**
   * Receives the trimmed draft once typing pauses for `delayMs`. The caller is expected to write it
   * where `value` comes from (the URL); that echo is recognised and never resets the field.
   */
  onCommit: (value: string) => void
  /** Pause after the last keystroke before committing. */
  delayMs?: number
  placeholder?: string
  'aria-label': string
  maxW?: string
  size?: 'sm' | 'md'
}

/**
 * DebouncedSearchInput is the free-text filter for list pages whose query lives in the URL: the
 * Server list discovery search, the OS image catalog search, and the Workflow kind filter. The
 * operator types into a local draft; after a pause the trimmed draft is committed so the page can
 * filter and the URL stays shareable.
 *
 * The field is never remounted when its own commit lands. Keying it on the committed value used to
 * rebuild the `<input>` on every commit, which dropped focus, discarded keystrokes typed while the
 * commit was in flight, and stripped the space between words. A trailing space now stays in the
 * draft while only the trimmed query reaches `onCommit`.
 *
 * A `value` this field did not commit (Clear filters, history navigation, a shared link) replaces
 * the draft, so the field always shows the filter the list is actually using.
 */
export function DebouncedSearchInput({
  value,
  onCommit,
  delayMs = 300,
  placeholder,
  maxW,
  size,
  'aria-label': ariaLabel,
}: DebouncedSearchInputProps) {
  const [draft, setDraft] = useState(value)
  const [observedValue, setObservedValue] = useState(value)
  // Queries this field committed whose echo has not reached `value` yet. React Router applies URL
  // updates as transitions, so a render can still carry the old `value` after a commit; comparing
  // against this list, rather than against the last commit alone, keeps an echo that arrives late
  // or out of order from being mistaken for an outside change that resets the draft.
  const [inFlight, setInFlight] = useState<readonly string[]>([])
  // Adjusting state while rendering is React's pattern for reacting to a changed prop without an
  // extra effect pass: each new `value` is either this field's own echo or an outside change.
  if (value !== observedValue) {
    setObservedValue(value)
    const echo = inFlight.indexOf(value)
    if (echo >= 0) {
      setInFlight(inFlight.slice(echo + 1))
    } else {
      setInFlight([])
      setDraft(value)
    }
  }
  const target = inFlight.length > 0 ? inFlight[inFlight.length - 1] : value

  // Commits the trimmed draft once typing pauses. Every keystroke restarts the timer and unmount
  // cancels it, so a query is never committed for a page the operator already left.
  useEffect(() => {
    const next = draft.trim()
    if (next === target) return
    const handle = setTimeout(() => {
      setInFlight((current) => [...current, next])
      onCommit(next)
    }, delayMs)
    return () => clearTimeout(handle)
  }, [draft, target, onCommit, delayMs])

  return (
    <SearchInput
      value={draft}
      onChange={setDraft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      maxW={maxW}
      size={size}
    />
  )
}
