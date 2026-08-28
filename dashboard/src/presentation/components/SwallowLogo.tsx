import type { SVGProps } from 'react'

interface SwallowLogoProps {
  className?: SVGProps<SVGSVGElement>['className']
}

/**
 * Three-quarter flying product mark used beside a visible Swallow wordmark.
 *
 * The raised wings, white breast, right-facing head, warm beak, and deeply
 * forked tail stay distinct at masthead size. The mark remains decorative
 * because every placement already exposes the product name.
 */
export function SwallowLogo({ className }: SwallowLogoProps) {
  return (
    <svg
      className={['sw-swallow-logo', className].filter(Boolean).join(' ')}
      viewBox="0 0 104 88"
      aria-hidden="true"
      focusable="false"
    >
      <path className="sw-swallow-logo__primary" d="M43 53C32 63 19 75 6 87l31-37-12 36 29-30Z" />
      <path
        className="sw-swallow-logo__primary"
        d="M52 43C38 38 23 29 9 19c-2-2 0-4 3-3l14 6-12-11c-2-2 0-4 3-3l17 11-9-13c-1-3 2-4 4-2 10 8 18 18 24 30Z"
      />
      <path
        className="sw-swallow-logo__primary"
        d="M50 44c3-12 8-23 14-32 2-3 4-2 4 2 3-6 7-10 11-13 3-2 4 0 3 4l6-5c2-1 4 0 3 3-4 18-14 34-30 47Z"
      />
      <path
        className="sw-swallow-logo__primary"
        d="M35 48c10-9 23-14 38-14 13 0 22 6 26 15l-8 3c-1 10-10 18-22 20-14 2-28-2-38-11 4-4 6-9 4-13Z"
      />
      <path
        className="sw-swallow-logo__light"
        d="M39 49c11-1 22-5 33-11 10-1 18 5 18 13 0 9-8 16-20 19-13 3-27-1-36-9 5-3 7-7 5-12Z"
      />
      <path className="sw-swallow-logo__accent" d="m90 47 13 6-12 5Z" />
      <circle className="sw-swallow-logo__primary sw-swallow-logo__eye" cx="81" cy="46" r="3" />
      <circle className="sw-swallow-logo__glint" cx="82" cy="45" r="0.9" />
    </svg>
  )
}
