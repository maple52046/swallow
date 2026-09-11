import { Box } from '@chakra-ui/react'
import { Tooltip } from '@/presentation/components/ui/tooltip'

interface GpuVendorLogoProps {
  vendor: string
  model?: string
}

type KnownVendor = 'amd' | 'nvidia' | 'intel' | 'generic'

/** Brand tint per vendor, resolved by color mode so marks stay legible on dark surfaces. */
const VENDOR_COLOR: Record<KnownVendor, { base: string; _dark: string } | undefined> = {
  amd: { base: '#c91920', _dark: '#ff6b70' },
  nvidia: { base: '#3e7b00', _dark: '#9bdc28' },
  intel: { base: '#0068b5', _dark: '#4cb5f5' },
  generic: undefined,
}

/** Maps provider names to the compact marks maintained by this component. */
function vendorKind(vendor: string): KnownVendor {
  const normalized = vendor.trim().toLowerCase()
  if (normalized.includes('amd') || normalized.includes('advanced micro devices')) return 'amd'
  if (normalized.includes('nvidia')) return 'nvidia'
  if (normalized.includes('intel')) return 'intel'
  return 'generic'
}

/** Draws vendor artwork while the wrapper owns tooltip and accessible naming. */
function VendorArtwork({ kind, label }: { kind: KnownVendor; label: string }) {
  if (kind === 'amd') {
    return <svg viewBox="0 0 76 24" aria-hidden="true" focusable="false"><text x="1" y="18" fontSize="17" fontWeight="700" fill="currentColor">AMD</text><path fill="currentColor" d="M54 3h19v19h-5V8H54V3Zm7 7h5v10H56v-5h5v-5Z" /></svg>
  }
  if (kind === 'nvidia') {
    return <svg viewBox="0 0 88 24" aria-hidden="true" focusable="false"><path fill="currentColor" d="M2 12c5-7 14-9 21-4-4-1-9 0-12 4 3 3 7 4 11 2-3 4-10 5-15 1l-5-3Zm9 0a4 4 0 1 1 8 0 4 4 0 0 1-8 0Z" /><text x="28" y="17" fontSize="11" fontWeight="700" fill="currentColor">NVIDIA</text></svg>
  }
  if (kind === 'intel') {
    return <svg viewBox="0 0 64 24" aria-hidden="true" focusable="false"><rect x="1" y="3" width="62" height="18" rx="3" fill="none" stroke="currentColor" /><text x="12" y="17" fontSize="15" fontWeight="700" fill="currentColor">intel</text></svg>
  }
  return <svg viewBox="0 0 72 24" aria-hidden="true" focusable="false"><rect x="1" y="3" width="70" height="18" rx="3" fill="none" stroke="currentColor" /><text x="36" y="17" textAnchor="middle" fontSize="11" fontWeight="700" fill="currentColor">{label.slice(0, 8).toUpperCase()}</text></svg>
}

/**
 * Compact SVG vendor wordmark for GPU-count cells.
 *
 * The wrapper supplies the accessible vendor name and a hover/focus tooltip with
 * model context; the SVG paths stay decorative to avoid duplicate announcements.
 * The mark is focusable so keyboard users can reach the model tooltip.
 */
export function GpuVendorLogo({ vendor, model }: GpuVendorLogoProps) {
  const label = vendor.trim() || 'Unknown'
  const kind = vendorKind(label)
  const tooltip = model ? label + ' ' + model : label
  return (
    <Tooltip content={tooltip}>
      <Box
        as="span"
        role="img"
        aria-label={label + ' GPU vendor logo'}
        tabIndex={0}
        display="inline-flex"
        alignItems="center"
        w="76px"
        h="24px"
        color={VENDOR_COLOR[kind] ?? 'fg'}
        css={{ '& svg': { width: '100%', height: '24px', overflow: 'visible' }, '& text': { fontFamily: 'var(--chakra-fonts-body)', letterSpacing: 0 } }}
      >
        <VendorArtwork kind={kind} label={label} />
      </Box>
    </Tooltip>
  )
}
