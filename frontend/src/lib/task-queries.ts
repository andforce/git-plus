import { queryOptions } from '@tanstack/react-query';
import { taskClient } from './connect/client';

export const taskRuntimeQueryOptions = queryOptions({
  queryKey: ['task', 'runtime'],
  queryFn: () => taskClient.getTaskRuntime({}),
});
