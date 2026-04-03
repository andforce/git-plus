import { Button, Card, Group, Text, Title } from '@mantine/core';
import { toast } from 'sonner';

export function CardsNotifications() {
  return (
    <Card>
      <Card.Section p="md" py="xs">
        <Title order={5}>Notifications</Title>
        <Text size="sm" c="dimmed">
          Click buttons to trigger different notification types.
        </Text>
      </Card.Section>
      <Card.Section p="md" py="xs">
        <Group gap="xs" w="100%" grow>
          <Button
            variant="light"
            color="gray"
            miw="fit-content"
            onClick={() => toast('Operation completed successfully!')}
          >
            Toast
          </Button>
          <Button
            variant="light"
            color="green"
            miw="fit-content"
            onClick={() => toast.success('Operation completed successfully!')}
          >
            Success
          </Button>
          <Button
            variant="light"
            color="red"
            miw="fit-content"
            onClick={() => toast.error('Something went wrong!')}
          >
            Error
          </Button>
          <Button
            variant="light"
            color="blue"
            miw="fit-content"
            onClick={() => toast.info('Here is some information.')}
          >
            Info
          </Button>
          <Button
            variant="light"
            color="yellow"
            miw="fit-content"
            onClick={() => toast.warning('Please be careful!')}
          >
            Warning
          </Button>
        </Group>
      </Card.Section>
      <Card.Section p="md" py="xs">
        <Group>
          <Button
            variant="outline"
            size="xs"
            onClick={() => {
              const id = toast.loading('Loading data...');
              setTimeout(() => {
                toast.success('Data loaded!', { id });
              }, 2000);
            }}
          >
            Loading
          </Button>
          <Button
            variant="outline"
            size="xs"
            onClick={() =>
              toast.promise(
                new Promise((resolve) => setTimeout(resolve, 2000)),
                {
                  loading: 'Processing...',
                  success: 'Task completed!',
                  error: 'Task failed!',
                },
              )
            }
          >
            Promise
          </Button>
          <Button
            variant="outline"
            size="xs"
            onClick={() =>
              toast('Custom notification', {
                description: 'This is a custom notification with description.',
                action: {
                  label: 'Undo',
                  onClick: () => toast.info('Undo clicked!'),
                },
              })
            }
          >
            With Action
          </Button>
          <Button
            variant="outline"
            size="xs"
            color="red"
            onClick={() => toast.dismiss()}
          >
            Dismiss All
          </Button>
        </Group>
      </Card.Section>
    </Card>
  );
}
