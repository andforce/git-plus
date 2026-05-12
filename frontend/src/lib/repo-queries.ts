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
const COMMITS_PAGE_SIZE = 50;

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

export function repoTreeQueryOptions(
  repoId: string,
  refName: string,
  path: string,
) {
  return queryOptions({
    queryKey: ['repo', 'tree', repoId, { refName, path }],
    queryFn: () =>
      repoClient.listTree({
        repoId: BigInt(repoId),
        refName,
        path,
      }),
  });
}

export function repoBlobQueryOptions(
  repoId: string,
  refName: string,
  path: string,
) {
  return queryOptions({
    queryKey: ['repo', 'blob', repoId, { refName, path }],
    queryFn: () =>
      repoClient.getBlob({
        repoId: BigInt(repoId),
        refName,
        path,
      }),
  });
}

export function repoBlameQueryOptions(
  repoId: string,
  refName: string,
  path: string,
) {
  return queryOptions({
    queryKey: ['repo', 'blame', repoId, { refName, path }],
    queryFn: () =>
      repoClient.getBlame({
        repoId: BigInt(repoId),
        refName,
        path,
      }),
  });
}

export function repoFileSearchQueryOptions(
  repoId: string,
  refName: string,
  query: string,
) {
  return queryOptions({
    queryKey: ['repo', 'file-search', repoId, { refName, query }],
    queryFn: () =>
      repoClient.searchFiles({
        repoId: BigInt(repoId),
        refName,
        query,
        pageSize: 50,
      }),
  });
}

export function repoCodeSearchQueryOptions(
  repoId: string,
  refName: string,
  query: string,
) {
  return queryOptions({
    queryKey: ['repo', 'code-search', repoId, { refName, query }],
    queryFn: () =>
      repoClient.searchCode({
        repoId: BigInt(repoId),
        refName,
        query,
        pageSize: 50,
      }),
  });
}

export function repoCommitsQueryOptions(
  repoId: string,
  refName: string,
  path: string = '',
) {
  return infiniteQueryOptions({
    queryKey: ['repo', 'commits', repoId, { refName, path }],
    queryFn: ({ pageParam = '' }) =>
      repoClient.listCommits({
        repoId: BigInt(repoId),
        refName,
        pageSize: COMMITS_PAGE_SIZE,
        pageToken: pageParam,
        path,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
  });
}

export function repoCommitQueryOptions(repoId: string, commitHash: string) {
  return queryOptions({
    queryKey: ['repo', 'commit', repoId, commitHash],
    queryFn: () =>
      repoClient.getCommit({
        repoId: BigInt(repoId),
        commitHash,
      }),
  });
}

export function repoCompareQueryOptions(
  repoId: string,
  baseRefName: string,
  headRefName: string,
) {
  return queryOptions({
    queryKey: ['repo', 'compare', repoId, { baseRefName, headRefName }],
    queryFn: () =>
      repoClient.compareRefs({
        repoId: BigInt(repoId),
        baseRefName,
        headRefName,
        pageSize: COMMITS_PAGE_SIZE,
      }),
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
