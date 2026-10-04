import { useEffect, useState } from 'react'

/**
 * The current wall-clock time in epoch milliseconds, re-rendering the caller every `intervalMs`.
 * Used by ticking displays such as a running time; each caller owns one interval, cleared when
 * the interval changes or the component unmounts.
 */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(timer)
  }, [intervalMs])

  return now
}
