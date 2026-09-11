import type { CSSProperties } from 'react'

interface SwallowLogoProps {
  /** Optional inline overrides (e.g. sizing) applied to the root SVG. */
  style?: CSSProperties
}

/** The product mark's fixed brand blue — intentionally independent of the theme accent. */
const LOGO_BLUE = '#0066cc'

/**
 * Three-quarter flying product mark shown beside the visible "Swallow" wordmark.
 *
 * The mark keeps its original fixed colours and does not follow the theme accent:
 * a brand-blue body, a solid white belly, a warm beak, and an eye highlight that
 * reads against the current panel background (via Chakra CSS variables) in light or
 * dark. It is decorative (`aria-hidden`) because every placement already exposes the
 * product name.
 */
export function SwallowLogo({ style }: SwallowLogoProps) {
  return (
    <svg
      viewBox="0 0 104 88"
      aria-hidden="true"
      focusable="false"
      style={{ width: '3rem', height: '2.5rem', flex: '0 0 auto', ...style }}
    >
      <path fill={LOGO_BLUE} d="M43 53C32 63 19 75 6 87l31-37-12 36 29-30Z" />
      <path
        fill={LOGO_BLUE}
        d="M52 43C38 38 23 29 9 19c-2-2 0-4 3-3l14 6-12-11c-2-2 0-4 3-3l17 11-9-13c-1-3 2-4 4-2 10 8 18 18 24 30Z"
      />
      <path
        fill={LOGO_BLUE}
        d="M50 44c3-12 8-23 14-32 2-3 4-2 4 2 3-6 7-10 11-13 3-2 4 0 3 4l6-5c2-1 4 0 3 3-4 18-14 34-30 47Z"
      />
      <path
        fill={LOGO_BLUE}
        d="M35 48c10-9 23-14 38-14 13 0 22 6 26 15l-8 3c-1 10-10 18-22 20-14 2-28-2-38-11 4-4 6-9 4-13Z"
      />
      <path
        fill="#fff"
        stroke={LOGO_BLUE}
        strokeLinejoin="round"
        strokeWidth="1.75"
        d="M39 49c11-1 22-5 33-11 10-1 18 5 18 13 0 9-8 16-20 19-13 3-27-1-36-9 5-3 7-7 5-12Z"
      />
      <path fill="var(--chakra-colors-orange-500)" d="m90 47 13 6-12 5Z" />
      <circle
        fill={LOGO_BLUE}
        stroke="var(--chakra-colors-bg-panel)"
        strokeWidth="1.25"
        cx="81"
        cy="46"
        r="3"
      />
      <circle fill="var(--chakra-colors-bg-panel)" cx="82" cy="45" r="0.9" />
    </svg>
  )
}
