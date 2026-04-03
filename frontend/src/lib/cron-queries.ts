import { queryOptions } from '@tanstack/react-query';
import { cronClient } from './connect/client';

export const cronRuntimeQueryOptions = queryOptions({
  queryKey: ['cron', 'runtime'],
  queryFn: () => cronClient.getCronRuntime({}),
});
