import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query';
import { repoClient } from './connect/client';

function encodePageToken(offset: number): string {
  return btoa(String(offset))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

const PAGE_SIZE = 30;
const CHANGES_PAGE_SIZE = 50;

export function repoListQueryOptions(
  search: string,
  sourceId: string,
  sort: string = 'created_at_desc',
) {
  return infiniteQueryOptions({
    queryKey: ['repo', 'list', { search, sourceId, sort }],
    queryFn: ({ pageParam = 0 }) =>
      repoClient.listRepositories({
        pageSize: PAGE_SIZE,
        pageToken: pageParam > 0 ? encodePageToken(pageParam) : '',
        search,
        sourceId,
        sort,
      }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, _allPages, lastPageParam) => {
      if (lastPage.nextPageToken) {
        return lastPageParam + PAGE_SIZE;
      }
      return undefined;
    },
  });
}

export function repoDetailQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['repo', 'detail', id],
    queryFn: () => repoClient.getRepository({ id: BigInt(id) }),
  });
}

export function repoRefsQueryOptions(
  repoId: string,
  refKind: 'head' | 'tag',
  includeDeleted = false,
) {
  return queryOptions({
    queryKey: ['repo', 'refs', repoId, refKind, { includeDeleted }],
    queryFn: () =>
      repoClient.listRefs({
        repoId: BigInt(repoId),
        refKind,
        includeDeleted,
      }),
  });
}

export function repoRefChangesQueryOptions(repoId: string, refName = '') {
  return infiniteQueryOptions({
    queryKey: ['repo', 'changes', repoId, { refName }],
    queryFn: ({ pageParam = 0 }) =>
      repoClient.listRefChanges({
        repoId: BigInt(repoId),
        refName,
        pageSize: CHANGES_PAGE_SIZE,
        pageToken: pageParam > 0 ? encodePageToken(pageParam) : '',
      }),
    initialPageParam: 0,
    getNextPageParam: (lastPage, _allPages, lastPageParam) => {
      if (lastPage.nextPageToken) {
        return lastPageParam + CHANGES_PAGE_SIZE;
      }
      return undefined;
    },
  });
}

export const repoCountQueryOptions = queryOptions({
  queryKey: ['repo', 'count'],
  queryFn: () =>
    repoClient.listRepositories({
      pageSize: 1,
      search: '',
      sourceId: '',
      sort: 'created_at_desc',
    }),
  staleTime: 60_000,
});
