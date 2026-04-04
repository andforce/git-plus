import { queryOptions } from '@tanstack/react-query';
import { taskClient } from './connect/client';

function encodePageToken(offset: number): string {
  return btoa(String(offset))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

export const taskRuntimeQueryOptions = queryOptions({
  queryKey: ['task', 'runtime'],
  queryFn: () => taskClient.getTaskRuntime({}),
});

export function taskRunsQueryOptions(page: number, pageSize = 20) {
  return queryOptions({
    queryKey: ['task', 'runs', { page, pageSize }],
    queryFn: () =>
      taskClient.listTaskRuns({
        pageSize,
        pageToken: page > 1 ? encodePageToken((page - 1) * pageSize) : '',
      }),
  });
}

export function taskRunQueryOptions(taskId: string) {
  return queryOptions({
    queryKey: ['task', 'run', taskId],
    queryFn: () => taskClient.getTaskRun({ taskId }),
  });
}

export function taskRunLogsQueryOptions(taskId: string) {
  return queryOptions({
    queryKey: ['task', 'run', taskId, 'logs'],
    queryFn: () => taskClient.listTaskRunLogs({ taskId }),
  });
}
