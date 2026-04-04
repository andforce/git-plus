import { useState } from 'react';
import { createFileRoute } from '@tanstack/react-router';
import {
  Badge,
  Button,
  Card,
  Checkbox,
  Container,
  Group,
  Loader,
  Menu,
  Modal,
  Stack,
  Text,
  Title,
} from '@mantine/core';
import {
  IconChevronDown,
  IconClock,
  IconRefresh,
  IconServer,
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
import { configClient, taskClient } from '~lib/connect/client';
import { configQueryOptions } from '~lib/config-queries';
import { taskRuntimeQueryOptions } from '~lib/task-queries';
import { useTaskEvents } from '~lib/use-task-events';
import { TaskEnqueueResult } from '~rpc/gitplus/task/v1/task_pb';

export const Route = createFileRoute('/_dashboard/maintenance/tasks')({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(taskRuntimeQueryOptions),
      queryClient.ensureQueryData(configQueryOptions),
    ]),
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
  const { data: configData } = useSuspenseQuery(configQueryOptions);
  const queryClient = useQueryClient();

  useTaskEvents();

  const sources = configData.config?.sources ?? [];
  const [syncSourceOpened, setSyncSourceOpened] = useState(false);
  const [selectedSources, setSelectedSources] = useState<Array<string>>([]);

  const toggleSource = (id: string) => {
    setSelectedSources((prev) =>
      prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id],
    );
  };

  const openSyncSourceModal = () => {
    setSelectedSources([]);
    setSyncSourceOpened(true);
  };

  const syncSourceMutation = useMutation({
    mutationFn: async (sourceIds: Array<string>) => {
      const results = await Promise.all(
        sourceIds.map((sourceId) => taskClient.enqueueSourceSync({ sourceId })),
      );
      return results;
    },
    onSuccess: (results) => {
      queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });
      setSyncSourceOpened(false);
      const started = results.filter(
        (r) => r.result === TaskEnqueueResult.STARTED,
      ).length;
      const queued = results.filter(
        (r) => r.result === TaskEnqueueResult.QUEUED,
      ).length;
      const deduped = results.filter(
        (r) => r.result === TaskEnqueueResult.DEDUPED,
      ).length;
      const parts: Array<string> = [];
      if (started > 0) parts.push(`${started} started`);
      if (queued > 0) parts.push(`${queued} queued`);
      if (deduped > 0) parts.push(`${deduped} deduped`);
      toast.success(`Source sync: ${parts.join(', ')}`);
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

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
          <Group gap={0}>
            <Button
              leftSection={<IconRefresh size={16} />}
              onClick={() => syncAllMutation.mutate()}
              loading={syncAllMutation.isPending}
              style={{
                borderTopRightRadius: 0,
                borderBottomRightRadius: 0,
              }}
            >
              Sync All
            </Button>
            <Menu position="bottom-end" withArrow shadow="md">
              <Menu.Target>
                <Button
                  px={8}
                  style={{
                    borderTopLeftRadius: 0,
                    borderBottomLeftRadius: 0,
                    borderLeft: '1px solid var(--mantine-primary-color-light)',
                  }}
                >
                  <IconChevronDown size={14} />
                </Button>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Item
                  leftSection={<IconServer size={14} />}
                  onClick={openSyncSourceModal}
                >
                  Sync Source...
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
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
      <Modal
        opened={syncSourceOpened}
        onClose={() => setSyncSourceOpened(false)}
        title="Sync Source"
        centered
      >
        {sources.length === 0 ? (
          <Text size="sm" c="dimmed">
            No sources configured. Add sources in Configuration first.
          </Text>
        ) : (
          <Stack gap="md">
            <Stack gap="xs">
              {sources.map((source) => (
                <Checkbox
                  key={source.id}
                  label={`${source.id} — @${source.username}`}
                  checked={selectedSources.includes(source.id)}
                  onChange={() => toggleSource(source.id)}
                />
              ))}
            </Stack>
            <Group justify="flex-end">
              <Button
                variant="default"
                onClick={() => setSyncSourceOpened(false)}
              >
                Cancel
              </Button>
              <Button
                onClick={() => syncSourceMutation.mutate(selectedSources)}
                loading={syncSourceMutation.isPending}
                disabled={selectedSources.length === 0}
              >
                Sync{' '}
                {selectedSources.length > 0
                  ? `(${selectedSources.length})`
                  : ''}
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
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
