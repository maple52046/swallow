import { ChakraProvider } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import { system } from '@/presentation/app/theme/system'
import { ColorModeProvider } from './color-mode'

interface ProviderProps {
  children: ReactNode
}

/**
 * Root styling provider for the console.
 *
 * Composes the Chakra styling engine (`system`) with the `next-themes`-backed
 * color-mode provider. Mount this once at the top of the tree (see `main.tsx`);
 * every Chakra component and semantic token resolves through it. It owns styling
 * only — application data providers (DI container, auth, router) sit inside it.
 */
export function Provider({ children }: ProviderProps) {
  return (
    <ChakraProvider value={system}>
      <ColorModeProvider>{children}</ColorModeProvider>
    </ChakraProvider>
  )
}
