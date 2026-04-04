import { Link, createFileRoute } from '@tanstack/react-router';
import {
  Badge,
  Box,
  Breadcrumbs,
  Container,
  Divider,
  Group,
  ScrollArea,
  Text,
  Title,
} from '@mantine/core';
import { useSuspenseQuery } from '@tanstack/react-query';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import dayjs from 'dayjs';
import type { TaskRunLog } from '~rpc/gitplus/task/v1/task_pb';
import { TaskState } from '~rpc/gitplus/task/v1/task_pb';
import {
  taskRunLogsQueryOptions,
  taskRunQueryOptions,
} from '~lib/task-queries';
import { useTaskEvents } from '~lib/use-task-events';

export const Route = createFileRoute('/_dashboard/maintenance/tasks/$taskId')({
  loader: ({ context: { queryClient }, params: { taskId } }) =>
    Promise.all([
      queryClient.ensureQueryData(taskRunQueryOptions(taskId)),
      queryClient.ensureQueryData(taskRunLogsQueryOptions(taskId)),
    ]),
  component: TaskDetailPage,
});

function formatTimestamp(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return '—';
  return dayjs(timestampDate(ts)).format('YYYY-MM-DD HH:mm:ss');
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

function formatLogLine(log: TaskRunLog): string {
  const ts = log.createdAt
    ? dayjs(timestampDate(log.createdAt)).format('HH:mm:ss.SSS')
    : '??:??:??.???';
  const tag = log.eventType.padEnd(10);
  const parts = [ts, tag];
  if (log.summary) parts.push(log.summary);
  if (log.errorMessage) parts.push(`ERROR: ${log.errorMessage}`);
  return parts.join('  ');
}

function TaskDetailPage() {
  const { taskId } = Route.useParams();
  const { data: runData } = useSuspenseQuery(taskRunQueryOptions(taskId));
  const { data: logsData } = useSuspenseQuery(taskRunLogsQueryOptions(taskId));

  const task = runData.taskRun;
  const logs = logsData.logs;

  const isTerminal =
    task?.state === TaskState.FINISHED || task?.state === TaskState.FAILED;
  useTaskEvents(isTerminal ? undefined : taskId);

  if (!task) {
    return (
      <Container fluid py="xl" px="xl">
        <Text c="dimmed">Task not found.</Text>
      </Container>
    );
  }

  const duration =
    task.startedAt && task.finishedAt
      ? dayjs(timestampDate(task.finishedAt)).diff(
          dayjs(timestampDate(task.startedAt)),
          'second',
          true,
        )
      : null;

  return (
    <Container fluid py="xl" px="xl">
      <Breadcrumbs mb="lg" separator="/">
        <Link
          to="/maintenance/tasks"
          style={{
            color: 'var(--mantine-color-dimmed)',
            fontSize: 'var(--mantine-font-size-sm)',
          }}
        >
          Tasks
        </Link>
        <Text size="sm">{task.name}</Text>
      </Breadcrumbs>

      <Group justify="space-between" align="flex-start" mb="xl">
        <div>
          <Title order={2}>{task.name}</Title>
          <Text size="sm" c="dimmed" ff="monospace" mt={4}>
            {task.taskId}
          </Text>
        </div>
        <Badge color={stateColor(task.state)} variant="light">
          {stateLabel(task.state)}
        </Badge>
      </Group>

      <Box
        style={{
          display: 'grid',
          gridTemplateColumns: 'auto auto',
          gap: 'var(--mantine-spacing-xs) var(--mantine-spacing-sm)',
          alignItems: 'baseline',
          width: 'fit-content',
        }}
      >
        <Text size="sm" c="dimmed">
          Job Type
        </Text>
        <Text size="sm" fw={500} ff="monospace">
          {task.jobType}
        </Text>

        <Text size="sm" c="dimmed">
          Started
        </Text>
        <Text size="sm" fw={500} ff="monospace">
          {formatTimestamp(task.startedAt)}
        </Text>

        <Text size="sm" c="dimmed">
          Finished
        </Text>
        <Text size="sm" fw={500} ff="monospace">
          {formatTimestamp(task.finishedAt)}
        </Text>

        {duration !== null && (
          <>
            <Text size="sm" c="dimmed">
              Duration
            </Text>
            <Text size="sm" fw={500} ff="monospace">
              {duration.toFixed(1)}s
            </Text>
          </>
        )}

        {task.errorMessage && (
          <>
            <Text size="sm" c="dimmed">
              Error
            </Text>
            <Text size="sm" fw={500} c="red">
              {task.errorMessage}
            </Text>
          </>
        )}

        {task.parentTaskId && (
          <>
            <Text size="sm" c="dimmed">
              Parent Task
            </Text>
            <Link
              to="/maintenance/tasks/$taskId"
              params={{ taskId: task.parentTaskId }}
              style={{ fontFamily: 'var(--mantine-font-family-monospace)' }}
            >
              {task.parentTaskId}
            </Link>
          </>
        )}

        {task.args && Object.keys(task.args).length > 0 && (
          <>
            <Text size="sm" c="dimmed">
              Args
            </Text>
            <Text size="sm" fw={500} ff="monospace">
              {JSON.stringify(task.args)}
            </Text>
          </>
        )}
      </Box>

      <Divider my="xl" />

      <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="md">
        Logs ({logs.length})
      </Text>

      {logs.length === 0 ? (
        <Text size="sm" c="dimmed">
          No log entries.
        </Text>
      ) : (
        <Box
          style={{
            backgroundColor: 'var(--mantine-color-dark-8)',
            borderRadius: 'var(--mantine-radius-sm)',
            overflow: 'hidden',
          }}
        >
          <ScrollArea.Autosize mah={600}>
            <Box
              component="pre"
              style={{
                margin: 0,
                padding: 'var(--mantine-spacing-md)',
                color: 'var(--mantine-color-dark-0)',
                fontFamily: 'var(--mantine-font-family-monospace)',
                fontSize: 'var(--mantine-font-size-xs)',
                lineHeight: 1.7,
                whiteSpace: 'pre',
              }}
            >
              {logs.map((log) => formatLogLine(log)).join('\n')}
            </Box>
          </ScrollArea.Autosize>
        </Box>
      )}
    </Container>
  );
}
