interface HardwareBrandPattern {
  label: string
  pattern: RegExp
}

/**
 * Display-only brand aliases, ordered from specific product families to broader architecture
 * names. They shorten provider strings (a CPU model such as "AMD EPYC 9654 96-Core Processor", or a
 * PCI vendor name such as "Advanced Micro Devices, Inc. [AMD/ATI]") without deciding any
 * capability or behavior.
 */
const HARDWARE_BRAND_PATTERNS: readonly HardwareBrandPattern[] = [
  { label: 'Intel', pattern: /\bintel\b/i },
  { label: 'AMD', pattern: /\b(?:amd|advanced micro devices|epyc|ryzen|radeon|instinct)\b/i },
  { label: 'Ampere', pattern: /\b(?:ampere|altra)\b/i },
  { label: 'Arm', pattern: /\b(?:arm|aarch64|neoverse)\b/i },
  { label: 'Apple', pattern: /\bapple\b/i },
  { label: 'IBM', pattern: /\b(?:ibm|power(?:8|9|10|11))\b/i },
  { label: 'NVIDIA', pattern: /\b(?:nvidia|grace)\b/i },
  { label: 'Qualcomm', pattern: /\b(?:qualcomm|oryon)\b/i },
  { label: 'AWS', pattern: /\b(?:aws|graviton)\b/i },
  { label: 'Fujitsu', pattern: /\bfujitsu\b/i },
  { label: 'Cavium', pattern: /\bcavium\b/i },
]

/**
 * Reduces a provider-reported hardware string (a CPU model or a GPU vendor) to a short brand for
 * inventory presentation, or undefined when no known brand appears in it.
 *
 * This helper is intentionally display-only: shared UI must never derive hardware behavior from
 * a vendor string. The caller picks its own fallback (a neutral "CPU", or the provider's own
 * vendor text) and keeps the full unmodified string for a tooltip.
 */
export function hardwareBrand(text: string): string | undefined {
  const normalized = text.trim()
  if (!normalized) return undefined
  return HARDWARE_BRAND_PATTERNS.find(({ pattern }) => pattern.test(normalized))?.label
}
