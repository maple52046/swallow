import { Flex, Text } from '@radix-ui/themes'

/** The application footer shown at the bottom of the authenticated layout. */
export function Footer() {
  return (
    <Flex height="100%" px="4" align="center" justify="between">
      <Text size="1" color="gray">
        © 2025 DC Dashboard. All rights reserved.
      </Text>
      <Text size="1" color="gray">
        v0.1.0-demo
      </Text>
    </Flex>
  )
}
