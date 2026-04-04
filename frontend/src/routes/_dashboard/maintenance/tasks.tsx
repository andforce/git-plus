import { createFileRoute } from '@tanstack/react-router';
import {
  Badge,
  Button,
  Card,
  Container,
  Group,
  Loader,
  Stack,
  Text,
  Title,
} from '@mantine/core';
import {
  IconClock,
  IconRefresh,
  IconTestPipe,
  IconX,
} from '@tabler/icons-react';
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query';
import { ConnectError } from '@connectrpc/connect';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { toast } from 'sonner';
import dayjs from 'dayjs';
import type { Task } from '~rpc/gitplus/task/v1/task_pb';
import { taskClient } from '~lib/connect/client';
import { taskRuntimeQueryOptions } from '~lib/task-queries';
import { useTaskEvents } from '~lib/use-task-events';
import { TaskEnqueueResult } from '~rpc/gitplus/task/v1/task_pb';

export const Route = createFileRoute('/_dashboard/maintenance/tasks')({
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(taskRuntimeQueryOptions),
  component: TasksPage,
});

function getErrorMessage(error: unknown): string {
  if (error instanceof ConnectError) return error.message;
  return 'An unexpected error occurred';
}

function enqueueResultLabel(result: TaskEnqueueResult): string {
  switch (result) {
    case TaskEnqueueResult.STARTED:
      return 'started';
    case TaskEnqueueResult.QUEUED:
      return 'queued';
    case TaskEnqueueResult.DEDUPED:
      return 'already queued (deduped)';
    default:
      return 'enqueued';
  }
}

function formatTime(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return '';
  return dayjs(timestampDate(ts)).fromNow();
}

function randomVariant(): number {
  return Math.floor(Math.random() * 5) + 1;
}

function TasksPage() {
  const { data } = useSuspenseQuery(taskRuntimeQueryOptions);
  const queryClient = useQueryClient();

  useTaskEvents();

  const syncAllMutation = useMutation({
    mutationFn: () => taskClient.enqueueFullSync({}),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });
      toast.success(`Sync all ${enqueueResultLabel(response.result)}`);
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const testMutation = useMutation({
    mutationFn: () => taskClient.enqueueTestTask({ variant: randomVariant() }),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });
      toast.success(`Task ${enqueueResultLabel(response.result)}`);
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const cancelMutation = useMutation({
    mutationFn: (taskId: string) => taskClient.cancelQueuedTask({ taskId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });
      toast.success('Task canceled');
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const runningTask = data.runningTask;
  const queuedTasks = data.queuedTasks;
  const isEmpty = !runningTask && queuedTasks.length === 0;

  return (
    <Container fluid py="xl" px="xl">
      <Group justify="space-between" align="flex-start" mb="xl">
        <div>
          <Title order={2}>Tasks</Title>
          <Text c="dimmed" size="sm">
            Monitor running and queued background tasks
          </Text>
        </div>
        <Group gap="sm">
          <Button
            leftSection={<IconRefresh size={16} />}
            onClick={() => syncAllMutation.mutate()}
            loading={syncAllMutation.isPending}
          >
            Sync All
          </Button>
          <Button
            variant="default"
            leftSection={<IconTestPipe size={16} />}
            onClick={() => testMutation.mutate()}
            loading={testMutation.isPending}
          >
            Enqueue Test Task
          </Button>
        </Group>
      </Group>

      {isEmpty ? (
        <Stack gap="md" maw={400} mt="xl">
          <div>
            <Text fw={600} size="lg">
              No active tasks
            </Text>
            <Text c="dimmed" size="sm" mt="xs" lh={1.6}>
              Tasks run background operations like syncing repositories from
              your configured sources. Use the test button above to enqueue a
              demo task.
            </Text>
          </div>
        </Stack>
      ) : (
        <Stack gap="lg">
          {runningTask && <RunningTaskCard task={runningTask} />}
          {queuedTasks.length > 0 && (
            <div>
              <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
                Queued ({queuedTasks.length})
              </Text>
              <Stack gap="sm">
                {queuedTasks.map((t) => (
                  <QueuedTaskCard
                    key={t.taskId}
                    task={t}
                    onCancel={() => cancelMutation.mutate(t.taskId)}
                    canceling={cancelMutation.isPending}
                  />
                ))}
              </Stack>
            </div>
          )}
        </Stack>
      )}
    </Container>
  );
}

function RunningTaskCard({ task }: { task: Task }) {
  return (
    <Card withBorder radius="md" padding="md">
      <Group justify="space-between" mb="xs">
        <Group gap="xs">
          <Loader size={14} />
          <Text fw={600} size="sm">
            {task.name}
          </Text>
        </Group>
        <Badge color="blue" variant="light" size="sm">
          Running
        </Badge>
      </Group>
      {task.progress && (
        <Text size="sm" c="dimmed">
          {task.progress.summary}
        </Text>
      )}
      <Text size="xs" c="dimmed" mt="xs">
        Started {formatTime(task.startedAt)}
      </Text>
    </Card>
  );
}

function QueuedTaskCard({
  task,
  onCancel,
  canceling,
}: {
  task: Task;
  onCancel: () => void;
  canceling: boolean;
}) {
  return (
    <Card withBorder radius="md" padding="md">
      <Group justify="space-between">
        <Group gap="xs">
          <IconClock
            size={14}
            style={{ color: 'var(--mantine-color-dimmed)' }}
          />
          <Text fw={600} size="sm">
            {task.name}
          </Text>
          <Badge color="gray" variant="light" size="sm">
            Queued
          </Badge>
        </Group>
        <Button
          variant="subtle"
          color="red"
          size="compact-sm"
          leftSection={<IconX size={14} />}
          onClick={onCancel}
          loading={canceling}
        >
          Cancel
        </Button>
      </Group>
      <Text size="xs" c="dimmed" mt="xs">
        Created {formatTime(task.createdAt)}
      </Text>
    </Card>
  );
}
