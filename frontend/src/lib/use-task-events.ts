import { create } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { eventClient } from './connect/client';
import type { JsonObject } from '@bufbuild/protobuf';
import type {
  GetTaskRunResponse,
  GetTaskRuntimeResponse,
  ListTaskRunLogsResponse,
  Task,
  TaskRunLog,
} from '~rpc/gitplus/task/v1/task_pb';
import {
  GetTaskRunResponseSchema,
  GetTaskRuntimeResponseSchema,
  ListTaskRunLogsResponseSchema,
  TaskSchema,
  TaskState,
} from '~rpc/gitplus/task/v1/task_pb';

const STATE_CHANGE_EVENTS = new Set([
  'task.enqueued',
  'task.started',
  'task.canceled',
  'task.finished',
  'task.failed',
]);

function asObject(value: unknown): JsonObject | undefined {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as JsonObject)
    : undefined;
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function parseTimestamp(value: unknown) {
  const iso = asString(value);
  if (!iso) return undefined;

  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return undefined;

  return timestampFromDate(date);
}

function parseTaskState(value: unknown): TaskState {
  switch (value) {
    case 'queued':
      return TaskState.QUEUED;
    case 'running':
      return TaskState.RUNNING;
    case 'finished':
      return TaskState.FINISHED;
    case 'failed':
    case 'canceled':
      return TaskState.FAILED;
    default:
      return TaskState.UNSPECIFIED;
  }
}

function parseTaskEvent(event: JsonObject | undefined): Task | undefined {
  const task = asObject(asObject(event?.data)?.task);
  if (!task) return undefined;

  const taskId = asString(task.task_id);
  const jobId = asString(task.job_id);
  const jobType = asString(task.job_type);
  const name = asString(task.name);

  if (!taskId || !jobId || !jobType || !name) return undefined;

  const progress = asObject(task.progress);

  return create(TaskSchema, {
    taskId,
    parentTaskId: asString(task.parent_task_id) ?? '',
    jobId,
    jobType,
    name,
    args: asObject(task.args),
    state: parseTaskState(task.state),
    createdAt: parseTimestamp(task.created_at),
    startedAt: parseTimestamp(task.started_at),
    finishedAt: parseTimestamp(task.finished_at),
    errorMessage: asString(task.error_message) ?? '',
    progress: progress
      ? {
          summary: asString(progress.summary) ?? '',
          meta: asObject(progress.meta),
          updatedAt: parseTimestamp(progress.updated_at),
        }
      : undefined,
  });
}

function createTaskLogFromEvent(
  event: JsonObject | undefined,
  taskId: string,
): TaskRunLog | undefined {
  const eventName = asString(event?.event_name);
  if (eventName !== 'task.progress') return undefined;

  const task = asObject(asObject(event?.data)?.task);
  const progress = asObject(task?.progress);
  const summary = asString(progress?.summary);
  const errorMessage = asString(event?.error_message) ?? '';
  const createdAt =
    parseTimestamp(progress?.updated_at) ?? parseTimestamp(event?.occurred_at);

  if (!summary && !errorMessage) return undefined;

  const syntheticIdSource =
    asString(progress?.updated_at) ?? asString(event?.occurred_at);
  const syntheticIdMillis = syntheticIdSource
    ? Date.parse(syntheticIdSource)
    : Number.NaN;
  const syntheticId = Number.isNaN(syntheticIdMillis)
    ? 0n
    : BigInt(syntheticIdMillis);

  return {
    $typeName: 'gitplus.task.v1.TaskRunLog',
    id: syntheticId,
    taskId,
    eventType: 'progress',
    summary: summary ?? '',
    meta: asObject(progress?.meta),
    errorMessage,
    createdAt,
  };
}

function isSameLog(a: TaskRunLog, b: TaskRunLog): boolean {
  return (
    a.eventType === b.eventType &&
    a.summary === b.summary &&
    a.errorMessage === b.errorMessage &&
    a.createdAt?.seconds === b.createdAt?.seconds &&
    a.createdAt?.nanos === b.createdAt?.nanos
  );
}

function updateRuntimeTask(
  runtime: GetTaskRuntimeResponse,
  nextTask: Task,
): GetTaskRuntimeResponse {
  const runningTask =
    runtime.runningTask?.taskId === nextTask.taskId
      ? nextTask
      : runtime.runningTask;
  const queuedTasks = runtime.queuedTasks.map((task) =>
    task.taskId === nextTask.taskId ? nextTask : task,
  );

  if (
    runningTask === runtime.runningTask &&
    queuedTasks.every((task, index) => task === runtime.queuedTasks[index])
  ) {
    return runtime;
  }

  return create(GetTaskRuntimeResponseSchema, {
    ...runtime,
    runningTask,
    queuedTasks,
  });
}

export function useTaskEvents(watchTaskId?: string) {
  const queryClient = useQueryClient();
  const watchRef = useRef(watchTaskId);
  watchRef.current = watchTaskId;

  useEffect(() => {
    const controller = new AbortController();

    (async () => {
      try {
        for await (const event of eventClient.subscribe(
          { channel: 'task' },
          { signal: controller.signal },
        )) {
          const eventName = event.event?.['event_name'];
          const isStateChange =
            typeof eventName === 'string' && STATE_CHANGE_EVENTS.has(eventName);
          const nextTask = parseTaskEvent(event.event);

          if (isStateChange) {
            queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });
            queryClient.invalidateQueries({ queryKey: ['task', 'runs'] });
          } else if (nextTask) {
            queryClient.setQueryData<GetTaskRuntimeResponse>(
              ['task', 'runtime'],
              (current) =>
                current ? updateRuntimeTask(current, nextTask) : current,
            );
          }

          const currentWatch = watchRef.current;
          if (currentWatch && event.event?.['task_id'] === currentWatch) {
            if (isStateChange) {
              queryClient.invalidateQueries({
                queryKey: ['task', 'run', currentWatch],
              });
              queryClient.invalidateQueries({
                queryKey: ['task', 'run', currentWatch, 'logs'],
              });
              continue;
            }

            if (nextTask) {
              queryClient.setQueryData<GetTaskRunResponse>(
                ['task', 'run', currentWatch],
                (current) =>
                  create(GetTaskRunResponseSchema, {
                    ...current,
                    taskRun: nextTask,
                  }),
              );
            }

            const nextLog = createTaskLogFromEvent(event.event, currentWatch);
            if (nextLog) {
              queryClient.setQueryData<ListTaskRunLogsResponse>(
                ['task', 'run', currentWatch, 'logs'],
                (current) => {
                  const logs = current?.logs ?? [];
                  if (logs.at(-1) && isSameLog(logs.at(-1)!, nextLog)) {
                    return current;
                  }

                  return create(ListTaskRunLogsResponseSchema, {
                    logs: [...logs, nextLog],
                  });
                },
              );
            }
          }
        }
      } catch {
        // AbortError on unmount is expected; ignore all stream errors
        // since the query cache retains the last known state.
      }
    })();

    return () => {
      controller.abort();
    };
  }, [queryClient]);
}
