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
  sort: string = 'created_at_desc',
) {
  return queryOptions({
    queryKey: ['repo', 'list', { page, pageSize, search, sourceId, sort }],
    queryFn: () =>
      repoClient.listRepositories({
        pageSize,
        pageToken: page > 1 ? encodePageToken((page - 1) * pageSize) : '',
        search,
        sourceId,
        sort,
      }),
  });
}
