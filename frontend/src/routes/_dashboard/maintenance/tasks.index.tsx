import { useState } from 'react';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import {
  Badge,
  Button,
  Card,
  Checkbox,
  Container,
  Divider,
  Group,
  Loader,
  Menu,
  Pagination,
  Stack,
  Table,
  Text,
  Title,
} from '@mantine/core';
import { modals } from '@mantine/modals';
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
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query';
import { ConnectError } from '@connectrpc/connect';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { toast } from 'sonner';
import dayjs from 'dayjs';
import type { Task } from '~rpc/gitplus/task/v1/task_pb';
import { TaskEnqueueResult, TaskState } from '~rpc/gitplus/task/v1/task_pb';
import { taskClient } from '~lib/connect/client';
import { configQueryOptions } from '~lib/config-queries';
import {
  taskRunsQueryOptions,
  taskRuntimeQueryOptions,
} from '~lib/task-queries';
import { useTaskEvents } from '~lib/use-task-events';

const PAGE_SIZE = 20;

export const Route = createFileRoute('/_dashboard/maintenance/tasks/')({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(taskRuntimeQueryOptions),
      queryClient.ensureQueryData(configQueryOptions),
      queryClient.ensureQueryData(taskRunsQueryOptions(1, PAGE_SIZE)),
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

function formatTimestamp(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return '—';
  return dayjs(timestampDate(ts)).format('YYYY-MM-DD HH:mm:ss');
}

function randomVariant(): number {
  return Math.floor(Math.random() * 5) + 1;
}

function stateLabel(state: TaskState): string {
  switch (state) {
    case TaskState.RUNNING:
      return 'Running';
    case TaskState.QUEUED:
      return 'Queued';
    case TaskState.FINISHED:
      return 'Finished';
    case TaskState.FAILED:
      return 'Failed';
    default:
      return 'Unknown';
  }
}

function stateColor(state: TaskState): string {
  switch (state) {
    case TaskState.RUNNING:
      return 'blue';
    case TaskState.QUEUED:
      return 'gray';
    case TaskState.FINISHED:
      return 'teal';
    case TaskState.FAILED:
      return 'red';
    default:
      return 'gray';
  }
}

function TasksPage() {
  const { data } = useSuspenseQuery(taskRuntimeQueryOptions);
  const { data: configData } = useSuspenseQuery(configQueryOptions);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [page, setPage] = useState(1);

  const { data: runsData } = useQuery(taskRunsQueryOptions(page, PAGE_SIZE));

  useTaskEvents();

  const openTaskDetail = (taskId: string) =>
    navigate({
      to: '/maintenance/tasks/$taskId',
      params: { taskId },
    });

  const sources = configData.config?.sources ?? [];

  const openSyncSourceModal = () => {
    modals.open({
      title: 'Sync Source',
      centered: true,
      children: <SyncSourceContent sources={sources} />,
    });
  };

  const syncAllMutation = useMutation({
    mutationFn: () => taskClient.enqueueFullSync({}),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['task'] });
      toast.success(`Sync all ${enqueueResultLabel(response.result)}`);
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const testMutation = useMutation({
    mutationFn: () => taskClient.enqueueTestTask({ variant: randomVariant() }),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['task'] });
      toast.success(`Task ${enqueueResultLabel(response.result)}`);
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const cancelMutation = useMutation({
    mutationFn: (taskId: string) => taskClient.cancelQueuedTask({ taskId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['task'] });
      toast.success('Task canceled');
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const runningTask = data.runningTask;
  const queuedTasks = data.queuedTasks;
  const hasActiveTasks = !!runningTask || queuedTasks.length > 0;

  const taskRuns = runsData?.taskRuns ?? [];
  const totalCount = runsData?.totalCount ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));

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

      {hasActiveTasks && (
        <>
          <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
            Active
          </Text>
          <Stack gap="sm" mb="xl">
            {runningTask && (
              <RunningTaskCard
                task={runningTask}
                onOpen={() => openTaskDetail(runningTask.taskId)}
              />
            )}
            {queuedTasks.map((t) => (
              <QueuedTaskCard
                key={t.taskId}
                task={t}
                onCancel={() => cancelMutation.mutate(t.taskId)}
                canceling={cancelMutation.isPending}
              />
            ))}
          </Stack>
          <Divider mb="xl" />
        </>
      )}

      <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
        History {totalCount > 0 && `(${totalCount})`}
      </Text>
      {taskRuns.length === 0 ? (
        <Text size="sm" c="dimmed">
          No task runs recorded yet.
        </Text>
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>Type</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Started</Table.Th>
              <Table.Th>Finished</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {taskRuns.map((run) => (
              <Table.Tr
                key={run.taskId}
                onClick={() => openTaskDetail(run.taskId)}
                style={{ cursor: 'pointer' }}
              >
                <Table.Td>
                  <Text size="sm" fw={500} truncate maw={300}>
                    {run.name}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed" ff="monospace">
                    {run.jobType}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Badge
                    color={stateColor(run.state)}
                    variant="light"
                    size="sm"
                  >
                    {stateLabel(run.state)}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {formatTimestamp(run.startedAt)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {formatTimestamp(run.finishedAt)}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      {totalPages > 1 && (
        <Pagination
          total={totalPages}
          value={page}
          onChange={setPage}
          size="sm"
          mt="md"
        />
      )}
    </Container>
  );
}

function SyncSourceContent({
  sources,
}: {
  sources: Array<{ id: string; username: string }>;
}) {
  const [selected, setSelected] = useState<Array<string>>([]);
  const queryClient = useQueryClient();

  const toggle = (id: string) => {
    setSelected((prev) =>
      prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id],
    );
  };

  const syncMutation = useMutation({
    mutationFn: async (sourceIds: Array<string>) => {
      const results = await Promise.all(
        sourceIds.map((sourceId) => taskClient.enqueueSourceSync({ sourceId })),
      );
      return results;
    },
    onSuccess: (results) => {
      queryClient.invalidateQueries({ queryKey: ['task'] });
      modals.closeAll();
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

  if (sources.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No sources configured. Add sources in Configuration first.
      </Text>
    );
  }

  return (
    <Stack gap="md">
      <Stack gap="xs">
        {sources.map((source) => (
          <Checkbox
            key={source.id}
            label={`${source.id} — @${source.username}`}
            checked={selected.includes(source.id)}
            onChange={() => toggle(source.id)}
          />
        ))}
      </Stack>
      <Group justify="flex-end">
        <Button variant="default" onClick={() => modals.closeAll()}>
          Cancel
        </Button>
        <Button
          onClick={() => syncMutation.mutate(selected)}
          loading={syncMutation.isPending}
          disabled={selected.length === 0}
        >
          Sync {selected.length > 0 ? `(${selected.length})` : ''}
        </Button>
      </Group>
    </Stack>
  );
}

function RunningTaskCard({ task, onOpen }: { task: Task; onOpen: () => void }) {
  return (
    <Card
      withBorder
      radius="md"
      padding="md"
      onClick={onOpen}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          onOpen();
        }
      }}
      role="button"
      tabIndex={0}
      style={{ cursor: 'pointer' }}
    >
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
