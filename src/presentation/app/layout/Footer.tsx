import { Group, Text } from '@mantine/core'

export function Footer() {
  return (
    <Group h="100%" px="md" justify="space-between">
      <Text size="xs" c="dimmed">
        © 2025 DC Dashboard. All rights reserved.
      </Text>
      <Text size="xs" c="dimmed">
        v0.1.0-demo
      </Text>
    </Group>
  )
}
