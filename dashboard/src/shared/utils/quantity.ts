/**
 * Formats a provider-reported hardware quantity as a whole number, optionally scaled by `divisor`
 * and followed by `unit`: `formatQuantity(2000.398934016, 'GB')` is `"2000 GB"` and
 * `formatQuantity(524288, 'GiB', 1024)` is `"512 GiB"`.
 *
 * Shared by every view that prints Server hardware (the Servers page, the Platform deploy wizard)
 * so the same fact never reads `2000.398934016 GB` in one place and `2000 GB` in another. A zero,
 * negative, or non-finite value means the provider reported nothing, so it renders as an em dash,
 * never as a misleading `0`.
 */
export function formatQuantity(value: number, unit = '', divisor = 1): string {
  if (!Number.isFinite(value) || value <= 0) return '—'
  const quantity = Math.round(value / divisor)
  return unit ? `${quantity} ${unit}` : String(quantity)
}
