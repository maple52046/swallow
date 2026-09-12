import { createSystem, defaultConfig, defineConfig } from '@chakra-ui/react'

/**
 * Chakra styling engine for the Swallow operator console.
 *
 * This module is the single source of truth for the dashboard's visual language.
 * It layers a bespoke operator-console theme on top of Chakra's
 * `defaultConfig` (so every stock recipe keeps working) and is consumed only by
 * the composition root in `components/ui/provider`. Nothing in `domain/` or
 * `application/` may import it — it is a framework/UI detail.
 *
 * Design intent (a modern operational workspace, not a legacy admin console):
 * - Neutral base is a cool slate scale (the `gray` palette is overridden), so
 *   backgrounds, borders, and text read calm and low-chroma. Chakra's global
 *   semantic tokens (`bg`, `fg`, `border`, …) reference `gray`, so overriding the
 *   scale re-tones the whole app, light and dark, from one place.
 * - A single indigo accent (`brand`) is reserved for primary interaction,
 *   selection, focus, links, and key status — never full-bleed colour washes.
 * - Rounded surfaces, restrained elevation, and short transitions establish
 *   hierarchy without obscuring dense operational data.
 *
 * Colour is never the only signal: status meaning is always carried by text or an
 * icon in the components that consume these tokens.
 */
const config = defineConfig({
  theme: {
    tokens: {
      fonts: {
        // System-native sans avoids a webfont round-trip while still reading modern
        // on current OSes (SF / Segoe UI / Roboto). Swap here to adopt a hosted face.
        heading: {
          value:
            "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif",
        },
        body: {
          value:
            "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif",
        },
        mono: {
          value:
            "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace",
        },
      },
      // The shared scale keeps data surfaces and controls related without making
      // every element look like the same container.
      radii: {
        sm: { value: '0.5rem' },
        md: { value: '0.625rem' },
        lg: { value: '0.875rem' },
        xl: { value: '1.125rem' },
        '2xl': { value: '1.375rem' },
      },
      shadows: {
        panel: {
          value: '0 1px 2px rgba(15, 23, 42, 0.08), 0 12px 28px rgba(15, 23, 42, 0.08)',
        },
        floating: {
          value: '0 18px 45px rgba(15, 23, 42, 0.18)',
        },
      },
      colors: {
        // A clean, professional blue accent that pops against the near-black canvas.
        // Used through the `brand` semantic palette below (and via
        // `colorPalette="brand"`), not referenced raw.
        brand: {
          50: { value: '#eff6ff' },
          100: { value: '#dbeafe' },
          200: { value: '#bfdbfe' },
          300: { value: '#93c5fd' },
          400: { value: '#60a5fa' },
          500: { value: '#3b82f6' },
          600: { value: '#2563eb' },
          700: { value: '#1d4ed8' },
          800: { value: '#1e40af' },
          900: { value: '#1e3a8a' },
          950: { value: '#172554' },
        },
        // Neutral cool-grey scale. The deep end (900/950) is near-black so the dark
        // canvas reads as black while panels/borders stay a hair lighter for depth.
        // Overriding `gray` re-tones every stock semantic token in both colour modes.
        gray: {
          50: { value: '#fafafa' },
          100: { value: '#f4f4f5' },
          200: { value: '#e4e4e7' },
          300: { value: '#d4d4d8' },
          400: { value: '#a1a1aa' },
          500: { value: '#71717a' },
          600: { value: '#52525b' },
          700: { value: '#3f3f46' },
          800: { value: '#27272a' },
          900: { value: '#171719' },
          950: { value: '#0a0a0b' },
        },
      },
    },
    semanticTokens: {
      colors: {
        // Wiring the accent into Chakra's palette contract makes
        // `colorPalette="brand"` light up primary buttons, links, focus rings, and
        // selected states. On the dark canvas the accent brightens one step (500) so
        // it reads crisp against near-black.
        brand: {
          solid: { value: { base: '{colors.brand.600}', _dark: '{colors.brand.500}' } },
          contrast: { value: { base: 'white', _dark: 'white' } },
          fg: { value: { base: '{colors.brand.700}', _dark: '{colors.brand.400}' } },
          muted: { value: { base: '{colors.brand.100}', _dark: '{colors.brand.900}' } },
          subtle: { value: { base: '{colors.brand.50}', _dark: '{colors.brand.950}' } },
          emphasized: { value: { base: '{colors.brand.200}', _dark: '{colors.brand.800}' } },
          focusRing: { value: { base: '{colors.brand.500}', _dark: '{colors.brand.400}' } },
        },
        // Canvas, panel, and raised surfaces are intentionally distinct so a page
        // can group content without outlining every nested element.
        bg: {
          DEFAULT: { value: { base: 'white', _dark: '{colors.gray.950}' } },
          canvas: { value: { base: '#f5f7fb', _dark: '#090b10' } },
          subtle: { value: { base: '#f5f7fb', _dark: '#090b10' } },
          muted: { value: { base: '#eef2f7', _dark: '#1c2029' } },
          emphasized: { value: { base: '{colors.gray.200}', _dark: '{colors.gray.700}' } },
          panel: { value: { base: 'white', _dark: '#14171e' } },
          raised: { value: { base: 'white', _dark: '#191d26' } },
        },
        border: {
          DEFAULT: { value: { base: '#e2e8f0', _dark: '#292e39' } },
          muted: { value: { base: '#edf1f6', _dark: '#222731' } },
        },
      },
    },
  },
  globalCss: {
    'html, body, #root': {
      height: '100%',
      minWidth: '320px',
    },
    body: {
      background: 'bg.canvas',
      backgroundImage:
        'radial-gradient(circle at 82% 0%, color-mix(in srgb, var(--chakra-colors-brand-solid) 8%, transparent), transparent 30rem)',
      backgroundAttachment: 'fixed',
      color: 'fg',
      textRendering: 'optimizeLegibility',
    },
    '::selection': {
      background: 'brand.muted',
      color: 'fg',
    },
    // Numeric identifiers (addresses, MACs, IDs) line up when tabular.
    '.sw-mono, .mono': {
      fontFamily: 'mono',
      fontVariantNumeric: 'tabular-nums',
    },
    // Lucide renders at 24px by default; scale to the current font size so glyphs
    // align with 1em text inside buttons, badges, and inline content.
    'svg.lucide': {
      width: '1em',
      height: '1em',
    },
  },
})

/**
 * The framework-agnostic styling engine passed to `ChakraProvider value={system}`.
 * Built once at module load and shared across the whole tree.
 */
export const system = createSystem(defaultConfig, config)
