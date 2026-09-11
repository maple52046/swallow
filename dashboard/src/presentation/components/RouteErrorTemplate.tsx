import { Box, EmptyState as ChakraEmptyState, Flex, HStack, Text, VStack } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import { SwallowLogo } from './SwallowLogo'

interface RouteErrorTemplateProps {
  /** Large status code shown for scannability (e.g. "403", "404"). */
  code: string
  title: string
  message: string
  /** Primary recovery control, typically a button back to a safe route. */
  action: ReactNode
}

/**
 * Branded full-page template shared by the 403 and 404 routes.
 *
 * Centres a titled empty-state on the calm canvas with the product mark pinned to
 * the corner, so unauthenticated or dead-end navigations still feel like part of
 * the console rather than a raw browser error.
 */
export function RouteErrorTemplate({ code, title, message, action }: RouteErrorTemplateProps) {
  return (
    <Flex as="main" minH="100dvh" align="center" justify="center" bg="bg.subtle" p="8" position="relative">
      <HStack position="absolute" top="6" insetStart="6" gap="2" color="brand.solid">
        <SwallowLogo />
        <Text as="strong" color="fg" fontWeight="bold">
          Swallow
        </Text>
      </HStack>
      <ChakraEmptyState.Root>
        <ChakraEmptyState.Content>
          <VStack gap="2" textAlign="center">
            <Box aria-hidden="true" fontSize="4xl" fontWeight="bold" color="fg.muted">
              {code}
            </Box>
            <ChakraEmptyState.Title>{title}</ChakraEmptyState.Title>
            <ChakraEmptyState.Description maxW="md">{message}</ChakraEmptyState.Description>
          </VStack>
          <Box mt="2">{action}</Box>
        </ChakraEmptyState.Content>
      </ChakraEmptyState.Root>
    </Flex>
  )
}
