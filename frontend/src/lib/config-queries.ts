import { queryOptions } from '@tanstack/react-query';
import { configClient } from './connect/client';

export const configQueryOptions = queryOptions({
  queryKey: ['config'],
  queryFn: () => configClient.getConfig({}),
});

export const configCheckQueryOptions = queryOptions({
  queryKey: ['config', 'check'],
  queryFn: () => configClient.checkConfig({}),
});
