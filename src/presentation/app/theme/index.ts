import { createTheme, type MantineColorsTuple } from '@mantine/core'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

const dcBlue: MantineColorsTuple = [
  '#e8f3ff', '#cce0ff', '#9fc0f9', '#6f9df3', '#4780ed',
  '#2d6de8', '#1f63e4', '#1252c9', '#0947b4', '#003ba0',
]

export const theme = createTheme({
  primaryColor: 'dcBlue',
  colors: { dcBlue },
  fontFamily: 'Inter, system-ui, -apple-system, sans-serif',
  fontFamilyMonospace: 'JetBrains Mono, Fira Code, Consolas, monospace',
  defaultRadius: 'md',
  components: {
    Button: { defaultProps: { radius: 'md' } },
    Badge: { defaultProps: { radius: 'sm' } },
    Card: { defaultProps: { radius: 'md', withBorder: true } },
  },
})

export type ColorScheme = 'light' | 'dark'

export function loadColorScheme(): ColorScheme {
  return lsGet<ColorScheme>('color-scheme', 'dark')
}

export function saveColorScheme(cs: ColorScheme): void {
  lsSet('color-scheme', cs)
}
