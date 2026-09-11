/** A browser appearance preference. `system` follows the live OS color-scheme setting. */
export type AppearanceMode = 'system' | 'light' | 'dark'

/** The concrete color scheme currently applied to the document. */
export type ResolvedAppearance = 'light' | 'dark'

export { system } from './system'
