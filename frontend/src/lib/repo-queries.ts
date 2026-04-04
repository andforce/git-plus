import { queryOptions } from '@tanstack/react-query';
import { repoClient } from './connect/client';

function encodePageToken(offset: number): string {
  return btoa(String(offset))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

export function repoListQueryOptions(
  page: number,
  pageSize: number,
  search: string,
  sourceId: string,
) {
  return queryOptions({
    queryKey: ['repo', 'list', { page, pageSize, search, sourceId }],
    queryFn: () =>
      repoClient.listRepositories({
        pageSize,
        pageToken: page > 1 ? encodePageToken((page - 1) * pageSize) : '',
        search,
        sourceId,
      }),
  });
}
