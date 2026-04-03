import {
  Button,
  Card,
  Divider,
  Group,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { IconBrandGithub, IconBrandGoogle } from '@tabler/icons-react';

export function CardsCreateAccount() {
  return (
    <Card>
      <Card.Section p="md" pb="xs">
        <Title order={3}>Create an account</Title>
        <Text size="sm" c="dimmed">
          Enter your email below to create your account
        </Text>
      </Card.Section>
      <Card.Section p="md" py="xs">
        <Group justify="space-between" grow>
          <Button variant="default" leftSection={<IconBrandGithub size={16} />}>
            GitHub
          </Button>
          <Button variant="default" leftSection={<IconBrandGoogle size={16} />}>
            Google
          </Button>
        </Group>
      </Card.Section>
      <Divider my="sm" label="OR CONTINUE WITH" labelPosition="center" />
      <Card.Section p="md" py="xs">
        <TextInput label="Email" placeholder="m@example.com" required />
        <TextInput label="Password" type="password" required mt="md" />
      </Card.Section>
      <Card.Section p="md" pt="xs">
        <Button fullWidth>Create account</Button>
      </Card.Section>
    </Card>
  );
}
