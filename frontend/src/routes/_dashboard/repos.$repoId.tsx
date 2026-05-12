import {
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { Link, createFileRoute } from '@tanstack/react-router';
import {
  ActionIcon,
  Alert,
  Anchor,
  Avatar,
  Badge,
  Box,
  Breadcrumbs,
  Button,
  Center,
  Checkbox,
  Code,
  Container,
  Drawer,
  Group,
  Image,
  Loader,
  Paper,
  Progress,
  ScrollArea,
  SegmentedControl,
  Select,
  SimpleGrid,
  Stack,
  Stepper,
  Table,
  Tabs,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import {
  useQuery,
  useSuspenseInfiniteQuery,
  useSuspenseQuery,
} from '@tanstack/react-query';
import { useDebouncedValue, useIntersection } from '@mantine/hooks';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import dayjs from 'dayjs';
import {
  IconAlertTriangle,
  IconArrowsExchange,
  IconBook,
  IconCheck,
  IconChevronRight,
  IconCircleX,
  IconCode,
  IconDownload,
  IconExternalLink,
  IconFile,
  IconFileText,
  IconFileZip,
  IconFolder,
  IconGitBranch,
  IconGitCommit,
  IconGitCompare,
  IconHistory,
  IconPackage,
  IconSearch,
  IconTag,
  IconX,
} from '@tabler/icons-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { toast } from 'sonner';
import type {
  CodeSearchMatch,
  CommitFileDiff,
  GetBlobResponse,
  RepoRef,
  RepoRefChange,
  RepositoryCommit,
  RepositoryReadme,
  StreamRepositoryDownloadResponse,
  TreeEntry,
} from '~rpc/gitplus/repo/v1/repo_pb';
import { DownloadStage, DownloadState } from '~rpc/gitplus/repo/v1/repo_pb';
import { repoClient } from '~lib/connect/client';
import { apiFetch } from '~lib/connect/transport';
import { configQueryOptions } from '~lib/config-queries';
import {
  downloadStageLabel,
  estimateProcessingTime,
  estimatedDownloadSize,
  formatEstimatedBytes,
} from '~lib/repo-download';
import {
  repoBlameQueryOptions,
  repoBlobQueryOptions,
  repoCodeSearchQueryOptions,
  repoCommitQueryOptions,
  repoCommitsQueryOptions,
  repoCompareQueryOptions,
  repoDetailQueryOptions,
  repoFileSearchQueryOptions,
  repoRefChangesQueryOptions,
  repoRefsQueryOptions,
  repoTreeQueryOptions,
} from '~lib/repo-queries';
import { sourcePrimaryLabel, sourceSecondaryLabel } from '~lib/source-display';

type RepoDetailTab =
  | 'code'
  | 'commits'
  | 'compare'
  | 'branches'
  | 'tags'
  | 'changes'
  | 'download';

type RepoDetailSearch = {
  tab?: RepoDetailTab;
  ref?: string;
  path?: string;
  file?: string;
  view?: RepoFileView;
  commit?: string;
  historyPath?: string;
  baseRef?: string;
  headRef?: string;
};

type RepoFileView = 'source' | 'blame';

const REPO_DETAIL_TABS = new Set<RepoDetailTab>([
  'code',
  'commits',
  'compare',
  'branches',
  'tags',
  'changes',
  'download',
]);

const REPO_FILE_VIEWS = new Set<RepoFileView>(['source', 'blame']);

export const Route = createFileRoute('/_dashboard/repos/$repoId')({
  validateSearch: (search: Record<string, unknown>): RepoDetailSearch =>
    normalizeRepoDetailSearch({
      tab: asString(search['tab']) as RepoDetailTab | undefined,
      ref: asString(search['ref']),
      path: asString(search['path']),
      file: asString(search['file']),
      view: asString(search['view']) as RepoFileView | undefined,
      commit: asString(search['commit']),
      historyPath: asString(search['historyPath']),
      baseRef: asString(search['baseRef']),
      headRef: asString(search['headRef']),
    }),
  loader: ({ context: { queryClient }, params: { repoId } }) =>
    queryClient.ensureQueryData(repoDetailQueryOptions(repoId)),
  component: RepoDetailPage,
});

function asString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value : undefined;
}

function normalizeRepoDetailSearch(
  search: Partial<RepoDetailSearch>,
): RepoDetailSearch {
  const tab =
    search.tab && REPO_DETAIL_TABS.has(search.tab) ? search.tab : undefined;

  return {
    tab,
    ref: cleanSearchValue(search.ref),
    path: cleanSearchPath(search.path),
    file: cleanSearchPath(search.file),
    view:
      search.view && REPO_FILE_VIEWS.has(search.view) ? search.view : undefined,
    commit: cleanSearchValue(search.commit),
    historyPath: cleanSearchPath(search.historyPath),
    baseRef: cleanSearchValue(search.baseRef),
    headRef: cleanSearchValue(search.headRef),
  };
}

function cleanSearchValue(value: string | undefined): string | undefined {
  const trimmed = value?.trim() ?? '';
  return trimmed || undefined;
}

function cleanSearchPath(value: string | undefined): string | undefined {
  const trimmed = value?.trim().replaceAll('\\', '/') ?? '';
  if (!trimmed || trimmed === '.' || trimmed === '/') return undefined;
  const parts = trimmed.split('/').filter((part) => part && part !== '.');
  const cleanParts = parts.filter((part) => part !== '..');
  return cleanParts.join('/') || undefined;
}

function useRepoDetailSearchUpdater() {
  const navigate = Route.useNavigate();

  return useCallback(
    (patch: Partial<RepoDetailSearch>) =>
      navigate({
        search: (previous) =>
          normalizeRepoDetailSearch({ ...previous, ...patch }),
        replace: false,
      }),
    [navigate],
  );
}

function tabSearchPatch(tab: RepoDetailTab): Partial<RepoDetailSearch> {
  switch (tab) {
    case 'code':
      return {
        tab,
        commit: undefined,
        historyPath: undefined,
        baseRef: undefined,
        headRef: undefined,
      };
    case 'commits':
      return {
        tab,
        path: undefined,
        file: undefined,
        view: undefined,
        commit: undefined,
        historyPath: undefined,
        baseRef: undefined,
        headRef: undefined,
      };
    case 'compare':
      return {
        tab,
        path: undefined,
        file: undefined,
        view: undefined,
        commit: undefined,
        historyPath: undefined,
      };
    default:
      return {
        tab,
        path: undefined,
        file: undefined,
        view: undefined,
        commit: undefined,
        historyPath: undefined,
        baseRef: undefined,
        headRef: undefined,
      };
  }
}

function formatTime(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return '—';
  return dayjs(timestampDate(ts)).format('YYYY-MM-DD HH:mm');
}

function formatTimeAgo(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return '—';
  return dayjs(timestampDate(ts)).fromNow();
}

function actionBadgeColor(action: string) {
  switch (action) {
    case 'create':
      return 'green';
    case 'update':
      return 'blue';
    case 'delete':
      return 'red';
    default:
      return 'gray';
  }
}

function shortHash(hash: string) {
  return hash ? hash.slice(0, 8) : '';
}

function stripRefPrefix(name: string) {
  return name.replace(/^refs\/(heads|tags)\//, '');
}

function truncateMessage(msg: string, max = 72) {
  const firstLine = msg.split('\n')[0];
  if (firstLine.length <= max) return firstLine;
  return firstLine.slice(0, max) + '...';
}

function formatBytes(value: bigint | number | undefined) {
  if (value === undefined) return '—';
  const bytes = typeof value === 'bigint' ? Number(value) : value;
  if (!Number.isFinite(bytes) || bytes <= 0) return bytes === 0 ? '0 B' : '—';

  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let current = bytes;
  let unitIndex = 0;
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024;
    unitIndex++;
  }

  return `${current >= 10 || unitIndex === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[unitIndex]}`;
}

function parentPath(repoPath: string) {
  const parts = repoPath.split('/').filter(Boolean);
  parts.pop();
  return parts.join('/');
}

function refDisplayName(refName: string) {
  return stripRefPrefix(refName);
}

function shortCommitHash(hash: string) {
  return hash ? hash.slice(0, 7) : '';
}

function isCommitHashLike(value: string | undefined) {
  return value ? /^[0-9a-f]{7,40}$/i.test(value.trim()) : false;
}

async function triggerRepositoryDownload(
  repoId: string,
  downloadId: string,
  filename: string,
) {
  const response = await apiFetch(
    `/api/repos/${repoId}/downloads/${downloadId}/archive`,
  );
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message.trim() || 'Failed to download repository archive');
  }

  const blob = await response.blob();
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = objectUrl;
  anchor.download = filename;
  anchor.style.display = 'none';
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(objectUrl);
}

function rawBlobApiPath(
  repoId: string,
  refName: string,
  repoPath: string,
  download = false,
) {
  const search = new URLSearchParams({
    ref: refName,
    path: repoPath,
  });
  if (download) search.set('download', '1');
  return `/api/repos/${repoId}/raw?${search.toString()}`;
}

async function fetchRawBlob(
  repoId: string,
  refName: string,
  repoPath: string,
  signal?: AbortSignal,
) {
  const response = await apiFetch(rawBlobApiPath(repoId, refName, repoPath), {
    signal,
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message.trim() || 'Failed to read file');
  }
  return response.blob();
}

async function triggerRawBlobOpen(
  repoId: string,
  refName: string,
  repoPath: string,
) {
  const opened = window.open('about:blank', '_blank');
  if (opened) {
    opened.document.title = 'Loading file...';
  }

  try {
    const blob = await fetchRawBlob(repoId, refName, repoPath);
    const objectUrl = URL.createObjectURL(blob);
    if (opened) {
      opened.location.href = objectUrl;
    } else {
      const anchor = document.createElement('a');
      anchor.href = objectUrl;
      anchor.target = '_blank';
      anchor.rel = 'noopener noreferrer';
      anchor.style.display = 'none';
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
    }
    setTimeout(() => URL.revokeObjectURL(objectUrl), 60_000);
  } catch (error) {
    opened?.close();
    throw error;
  }
}

async function triggerBlobFileDownload(
  repoId: string,
  refName: string,
  repoPath: string,
  filename: string,
) {
  const response = await apiFetch(
    rawBlobApiPath(repoId, refName, repoPath, true),
  );
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message.trim() || 'Failed to download file');
  }

  const blob = await response.blob();
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = objectUrl;
  anchor.download = filename;
  anchor.style.display = 'none';
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(objectUrl);
}

function isPreviewableImage(filename: string) {
  return /\.(apng|avif|bmp|gif|ico|jpe?g|png|svg|webp)$/i.test(filename);
}

function renderHashDisplay(change: RepoRefChange) {
  switch (change.action) {
    case 'create':
      return <Code>{shortHash(change.newHash)}</Code>;
    case 'delete':
      return (
        <Text size="xs" c="dimmed" span>
          <Code>{shortHash(change.oldHash)}</Code> (deleted)
        </Text>
      );
    case 'update':
      return (
        <Group gap={4}>
          <Code>{shortHash(change.oldHash)}</Code>
          <Text size="xs" c="dimmed" span>
            &rarr;
          </Text>
          <Code>{shortHash(change.newHash)}</Code>
        </Group>
      );
    default:
      return '—';
  }
}

function ChangeCommitCell({
  change,
  maxWidth,
}: {
  change: RepoRefChange;
  maxWidth?: number;
}) {
  const commit = change.newCommit;

  return (
    <Table.Td style={maxWidth ? { maxWidth } : undefined}>
      <Box>
        <Box fz="xs">{renderHashDisplay(change)}</Box>
        {commit?.message ? (
          <Text size="xs" c="dimmed" lineClamp={1}>
            {truncateMessage(commit.message)}
          </Text>
        ) : (
          <Text size="xs" c="dimmed">
            —
          </Text>
        )}
      </Box>
    </Table.Td>
  );
}

function TabFallback() {
  return (
    <Center py="xl">
      <Loader size="sm" />
    </Center>
  );
}

function useRepositoryRefOptions(repoId: string, defaultBranch: string) {
  const { data: branchesData } = useSuspenseQuery(
    repoRefsQueryOptions(repoId, 'head'),
  );
  const { data: tagsData } = useSuspenseQuery(
    repoRefsQueryOptions(repoId, 'tag'),
  );

  const refs = useMemo(
    () => [...branchesData.refs, ...tagsData.refs],
    [branchesData.refs, tagsData.refs],
  );

  const options = useMemo(
    () =>
      refs.map((ref) => ({
        value: ref.refName,
        label:
          ref.refKind === 'tag'
            ? `tag: ${refDisplayName(ref.refName)}`
            : refDisplayName(ref.refName),
      })),
    [refs],
  );

  const preferredRef = useMemo(() => {
    const defaultRefName = defaultBranch
      ? `refs/heads/${defaultBranch}`
      : undefined;
    return (
      refs.find((ref) => ref.refName === defaultRefName)?.refName ??
      refs.find((ref) => ref.refKind === 'head')?.refName ??
      refs[0]?.refName ??
      ''
    );
  }, [defaultBranch, refs]);

  return { refs, options, preferredRef };
}

function selectOptionsWithCurrentRef(
  options: Array<{ value: string; label: string }>,
  selectedRef: string,
) {
  if (!selectedRef || options.some((option) => option.value === selectedRef)) {
    return options;
  }
  return [
    {
      value: selectedRef,
      label: `commit: ${shortCommitHash(selectedRef)}`,
    },
    ...options,
  ];
}

function CodeTab({
  repoId,
  defaultBranch,
}: {
  repoId: string;
  defaultBranch: string;
}) {
  const search = Route.useSearch();
  const updateSearch = useRepoDetailSearchUpdater();
  const [fileSearchOpen, setFileSearchOpen] = useState(false);
  const [codeSearchOpen, setCodeSearchOpen] = useState(false);
  const { refs, options, preferredRef } = useRepositoryRefOptions(
    repoId,
    defaultBranch,
  );
  const selectedRef =
    refs.some((ref) => ref.refName === search.ref) ||
    isCommitHashLike(search.ref)
      ? (search.ref ?? '')
      : preferredRef;
  const selectOptions = useMemo(
    () => selectOptionsWithCurrentRef(options, selectedRef),
    [options, selectedRef],
  );
  const currentPath = search.path ?? '';
  const blobPath = search.file ?? null;
  const fileView = search.view ?? 'source';

  useEffect(() => {
    if (!preferredRef) return;
    if (
      !selectedRef ||
      (!refs.some((ref) => ref.refName === selectedRef) &&
        !isCommitHashLike(selectedRef))
    ) {
      updateSearch({
        tab: 'code',
        ref: preferredRef,
        path: undefined,
        file: undefined,
        view: undefined,
        commit: undefined,
      });
    }
  }, [preferredRef, refs, selectedRef, updateSearch]);

  const handleRefChange = (value: string | null) => {
    updateSearch({
      tab: 'code',
      ref: value ?? undefined,
      path: undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
    });
  };

  const openDirectory = (pathValue: string) => {
    updateSearch({
      tab: 'code',
      path: pathValue || undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
    });
  };

  const navigateToPath = (pathValue: string) => {
    updateSearch({
      tab: 'code',
      path: pathValue || undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
    });
  };

  if (refs.length === 0 || !selectedRef) {
    return (
      <Text size="sm" c="dimmed">
        No active branches or tags are available for browsing.
      </Text>
    );
  }

  const displayPath = blobPath ?? currentPath;

  return (
    <Stack gap="md">
      <Group justify="space-between" align="center">
        <Select
          data={selectOptions}
          value={selectedRef}
          onChange={handleRefChange}
          searchable
          allowDeselect={false}
          leftSection={<IconGitBranch size={14} />}
          w={{ base: '100%', sm: 280 }}
        />
        <Group gap="xs">
          <Button
            variant="default"
            size="xs"
            leftSection={<IconSearch size={14} />}
            onClick={() => setFileSearchOpen(true)}
          >
            Go to file
          </Button>
          <Button
            variant="default"
            size="xs"
            leftSection={<IconCode size={14} />}
            onClick={() => setCodeSearchOpen(true)}
          >
            Search code
          </Button>
          {blobPath && (
            <Button
              variant="default"
              size="xs"
              onClick={() =>
                updateSearch({
                  tab: 'code',
                  path: parentPath(blobPath) || undefined,
                  file: undefined,
                  view: undefined,
                  commit: undefined,
                })
              }
            >
              Back to directory
            </Button>
          )}
        </Group>
      </Group>

      <FileSearchDrawer
        opened={fileSearchOpen}
        onClose={() => setFileSearchOpen(false)}
        repoId={repoId}
        refName={selectedRef}
        onOpenFile={(pathValue) => {
          setFileSearchOpen(false);
          updateSearch({
            tab: 'code',
            path: undefined,
            file: pathValue,
            view: undefined,
            commit: undefined,
          });
        }}
      />

      <CodeSearchDrawer
        opened={codeSearchOpen}
        onClose={() => setCodeSearchOpen(false)}
        repoId={repoId}
        refName={selectedRef}
        onOpenFile={(pathValue) => {
          setCodeSearchOpen(false);
          updateSearch({
            tab: 'code',
            path: undefined,
            file: pathValue,
            view: undefined,
            commit: undefined,
          });
        }}
      />

      <RepositoryPathBar path={displayPath} onNavigate={navigateToPath} />

      {blobPath ? (
        <Suspense fallback={<TabFallback />}>
          <BlobContent
            repoId={repoId}
            refName={selectedRef}
            path={blobPath}
            view={fileView}
            onViewChange={(view) =>
              updateSearch({
                tab: 'code',
                view: view === 'source' ? undefined : view,
              })
            }
            onShowHistory={() =>
              updateSearch({
                tab: 'commits',
                ref: selectedRef,
                path: undefined,
                file: undefined,
                view: undefined,
                commit: undefined,
                historyPath: blobPath,
              })
            }
          />
        </Suspense>
      ) : (
        <Suspense fallback={<TabFallback />}>
          <TreeContent
            repoId={repoId}
            refName={selectedRef}
            path={currentPath}
            onOpenDirectory={openDirectory}
            onOpenFile={(pathValue) =>
              updateSearch({
                tab: 'code',
                path: undefined,
                file: pathValue,
                view: undefined,
                commit: undefined,
              })
            }
          />
        </Suspense>
      )}
    </Stack>
  );
}

function FileSearchDrawer({
  opened,
  onClose,
  repoId,
  refName,
  onOpenFile,
}: {
  opened: boolean;
  onClose: () => void;
  repoId: string;
  refName: string;
  onOpenFile: (path: string) => void;
}) {
  const [query, setQuery] = useState('');
  const [debouncedQuery] = useDebouncedValue(query, 150);
  const { data, isFetching, isError, error } = useQuery({
    ...repoFileSearchQueryOptions(repoId, refName, debouncedQuery),
    enabled: opened && Boolean(refName),
  });
  const entries = data?.entries ?? [];

  return (
    <Drawer
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="xs">
          <IconSearch size={16} />
          <Text fw={600}>Go to file</Text>
        </Group>
      }
      position="right"
      size="lg"
    >
      <Stack gap="md">
        <TextInput
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
          leftSection={<IconSearch size={14} />}
          placeholder="Search files"
          autoFocus
        />

        {isError ? (
          <Alert color="red" variant="light">
            {error instanceof Error ? error.message : 'File search failed.'}
          </Alert>
        ) : (
          <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
            <Table highlightOnHover>
              <Table.Tbody>
                {entries.map((entry) => (
                  <FileSearchRow
                    key={entry.path}
                    entry={entry}
                    onOpen={() => onOpenFile(entry.path)}
                  />
                ))}
                {entries.length === 0 && !isFetching && (
                  <Table.Tr>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        No files found.
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          </Paper>
        )}

        {isFetching && (
          <Center py="xs">
            <Loader size="sm" />
          </Center>
        )}
        {data?.isTruncated && (
          <Text size="xs" c="dimmed">
            Showing the first 50 matches.
          </Text>
        )}
      </Stack>
    </Drawer>
  );
}

function FileSearchRow({
  entry,
  onOpen,
}: {
  entry: TreeEntry;
  onOpen: () => void;
}) {
  return (
    <Table.Tr style={{ cursor: 'pointer' }} onClick={onOpen}>
      <Table.Td>
        <Group gap="xs" wrap="nowrap">
          {entryIcon(entry.kind)}
          <Text size="sm" truncate>
            {entry.path}
          </Text>
        </Group>
      </Table.Td>
      <Table.Td style={{ width: 1 }}>
        <Text size="xs" c="dimmed" ta="right">
          {formatBytes(entry.size)}
        </Text>
      </Table.Td>
    </Table.Tr>
  );
}

function CodeSearchDrawer({
  opened,
  onClose,
  repoId,
  refName,
  onOpenFile,
}: {
  opened: boolean;
  onClose: () => void;
  repoId: string;
  refName: string;
  onOpenFile: (path: string) => void;
}) {
  const [query, setQuery] = useState('');
  const [debouncedQuery] = useDebouncedValue(query, 200);
  const hasQuery = debouncedQuery.trim().length > 0;
  const { data, isFetching, isError, error } = useQuery({
    ...repoCodeSearchQueryOptions(repoId, refName, debouncedQuery),
    enabled: opened && Boolean(refName) && hasQuery,
  });
  const matches = data?.matches ?? [];

  return (
    <Drawer
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="xs">
          <IconCode size={16} />
          <Text fw={600}>Search code</Text>
        </Group>
      }
      position="right"
      size="xl"
    >
      <Stack gap="md">
        <TextInput
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
          leftSection={<IconSearch size={14} />}
          placeholder="Search code"
          autoFocus
        />

        {isError ? (
          <Alert color="red" variant="light">
            {error instanceof Error ? error.message : 'Code search failed.'}
          </Alert>
        ) : (
          <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
            <Table highlightOnHover>
              <Table.Tbody>
                {matches.map((match, index) => (
                  <CodeSearchRow
                    key={`${match.path}:${match.lineNo}:${index}`}
                    match={match}
                    onOpen={() => onOpenFile(match.path)}
                  />
                ))}
                {matches.length === 0 && !isFetching && (
                  <Table.Tr>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        {hasQuery
                          ? 'No code matches found.'
                          : 'Enter a search term.'}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          </Paper>
        )}

        {isFetching && (
          <Center py="xs">
            <Loader size="sm" />
          </Center>
        )}
        {data?.isTruncated && (
          <Text size="xs" c="dimmed">
            Showing the first 50 matches.
          </Text>
        )}
      </Stack>
    </Drawer>
  );
}

function CodeSearchRow({
  match,
  onOpen,
}: {
  match: CodeSearchMatch;
  onOpen: () => void;
}) {
  return (
    <Table.Tr style={{ cursor: 'pointer' }} onClick={onOpen}>
      <Table.Td>
        <Stack gap={2}>
          <Group gap="xs" wrap="nowrap">
            <IconFile size={14} color="var(--mantine-color-gray-6)" />
            <Text size="sm" fw={500} truncate>
              {match.path}
            </Text>
            <Code>{match.lineNo}</Code>
          </Group>
          <Code block className="repo-code-search-line">
            {match.line || ' '}
          </Code>
        </Stack>
      </Table.Td>
    </Table.Tr>
  );
}

function RepositoryPathBar({
  path: repoPath,
  onNavigate,
}: {
  path: string;
  onNavigate: (path: string) => void;
}) {
  const parts = repoPath.split('/').filter(Boolean);
  let runningPath = '';

  return (
    <Paper withBorder p="sm" radius="sm">
      <Group gap={6} wrap="nowrap" style={{ overflow: 'hidden' }}>
        <Anchor component="button" type="button" onClick={() => onNavigate('')}>
          root
        </Anchor>
        {parts.map((part, index) => {
          runningPath = runningPath ? `${runningPath}/${part}` : part;
          const targetPath = runningPath;
          const isLast = index === parts.length - 1;
          return (
            <Group key={targetPath} gap={6} wrap="nowrap">
              <IconChevronRight size={14} color="var(--mantine-color-dimmed)" />
              {isLast ? (
                <Text size="sm" fw={500} truncate>
                  {part}
                </Text>
              ) : (
                <Anchor
                  component="button"
                  type="button"
                  onClick={() => onNavigate(targetPath)}
                >
                  {part}
                </Anchor>
              )}
            </Group>
          );
        })}
      </Group>
    </Paper>
  );
}

function TreeContent({
  repoId,
  refName,
  path: repoPath,
  onOpenDirectory,
  onOpenFile,
}: {
  repoId: string;
  refName: string;
  path: string;
  onOpenDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
}) {
  const { data } = useSuspenseQuery(
    repoTreeQueryOptions(repoId, refName, repoPath),
  );

  return (
    <Stack gap="md">
      <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
        <Group justify="space-between" px="md" py="sm">
          <Text size="sm" fw={500}>
            {data.entries.length.toLocaleString()} items
          </Text>
          <Code>{shortCommitHash(data.commitHash)}</Code>
        </Group>
        <Table highlightOnHover>
          <Table.Tbody>
            {repoPath && (
              <Table.Tr
                style={{ cursor: 'pointer' }}
                onClick={() => onOpenDirectory(parentPath(repoPath))}
              >
                <Table.Td>
                  <Group gap="xs">
                    <IconFolder size={16} />
                    <Text size="sm">..</Text>
                  </Group>
                </Table.Td>
                <Table.Td />
                <Table.Td />
              </Table.Tr>
            )}
            {data.entries.map((entry) => (
              <TreeEntryRow
                key={entry.path}
                entry={entry}
                onOpenDirectory={onOpenDirectory}
                onOpenFile={onOpenFile}
              />
            ))}
          </Table.Tbody>
        </Table>
      </Paper>

      {data.readme && <ReadmePreview readme={data.readme} />}
    </Stack>
  );
}

function TreeEntryRow({
  entry,
  onOpenDirectory,
  onOpenFile,
}: {
  entry: TreeEntry;
  onOpenDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
}) {
  const isDirectory = entry.kind === 'directory';
  const isSubmodule = entry.kind === 'submodule';
  const handleClick = () => {
    if (isDirectory) {
      onOpenDirectory(entry.path);
      return;
    }
    if (!isSubmodule) {
      onOpenFile(entry.path);
    }
  };

  return (
    <Table.Tr
      style={{ cursor: isSubmodule ? 'default' : 'pointer' }}
      onClick={handleClick}
    >
      <Table.Td>
        <Group gap="xs" wrap="nowrap">
          {entryIcon(entry.kind)}
          <Text size="sm" fw={isDirectory ? 500 : 400} truncate>
            {entry.name}
          </Text>
        </Group>
      </Table.Td>
      <Table.Td>
        <Text size="xs" c="dimmed">
          {entry.kind}
        </Text>
      </Table.Td>
      <Table.Td>
        <Text size="xs" c="dimmed" ta="right">
          {entry.kind === 'file' || entry.kind === 'symlink'
            ? formatBytes(entry.size)
            : '—'}
        </Text>
      </Table.Td>
    </Table.Tr>
  );
}

function entryIcon(kind: string) {
  switch (kind) {
    case 'directory':
      return <IconFolder size={16} color="var(--mantine-color-blue-6)" />;
    case 'symlink':
      return <IconFileText size={16} color="var(--mantine-color-violet-6)" />;
    case 'submodule':
      return <IconPackage size={16} color="var(--mantine-color-orange-6)" />;
    default:
      return <IconFile size={16} color="var(--mantine-color-gray-6)" />;
  }
}

function BlobContent({
  repoId,
  refName,
  path: repoPath,
  view,
  onViewChange,
  onShowHistory,
}: {
  repoId: string;
  refName: string;
  path: string;
  view: RepoFileView;
  onViewChange: (view: RepoFileView) => void;
  onShowHistory: () => void;
}) {
  const { data } = useSuspenseQuery(
    repoBlobQueryOptions(repoId, refName, repoPath),
  );
  const effectiveView = data.isBinary && view === 'blame' ? 'source' : view;

  return (
    <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
      <Group justify="space-between" px="md" py="sm">
        <Group gap="xs">
          <IconFile size={16} />
          <Text size="sm" fw={500}>
            {data.name}
          </Text>
        </Group>
        <Group gap="sm">
          <SegmentedControl
            size="xs"
            value={effectiveView}
            onChange={(value) => onViewChange(value as RepoFileView)}
            data={[
              { label: 'Source', value: 'source' },
              { label: 'Blame', value: 'blame', disabled: data.isBinary },
            ]}
          />
          <Text size="xs" c="dimmed">
            {formatBytes(data.size)}
          </Text>
          <Code>{shortCommitHash(data.commitHash)}</Code>
          <Button
            variant="default"
            size="xs"
            leftSection={<IconExternalLink size={14} />}
            onClick={() =>
              toast.promise(triggerRawBlobOpen(repoId, refName, repoPath), {
                loading: 'Opening raw file...',
                success: 'Raw file opened',
                error: (error) =>
                  error instanceof Error
                    ? error.message
                    : 'Failed to open file',
              })
            }
          >
            Raw
          </Button>
          <Button
            variant="default"
            size="xs"
            leftSection={<IconDownload size={14} />}
            onClick={() =>
              toast.promise(
                triggerBlobFileDownload(repoId, refName, repoPath, data.name),
                {
                  loading: 'Preparing file download...',
                  success: 'File download started',
                  error: (error) =>
                    error instanceof Error
                      ? error.message
                      : 'Failed to download file',
                },
              )
            }
          >
            Download
          </Button>
          <Button
            variant="default"
            size="xs"
            leftSection={<IconHistory size={14} />}
            onClick={onShowHistory}
          >
            History
          </Button>
        </Group>
      </Group>
      <BlobPreview
        blob={data}
        repoId={repoId}
        refName={refName}
        view={effectiveView}
      />
    </Paper>
  );
}

function BlobPreview({
  blob,
  repoId,
  refName,
  view,
}: {
  blob: GetBlobResponse;
  repoId: string;
  refName: string;
  view: RepoFileView;
}) {
  if (view === 'blame') {
    return <BlamePreview repoId={repoId} refName={refName} path={blob.path} />;
  }

  if (isPreviewableImage(blob.name)) {
    return <ImageBlobPreview blob={blob} repoId={repoId} refName={refName} />;
  }

  if (blob.isBinary) {
    return (
      <Center py="xl">
        <Text size="sm" c="dimmed">
          Binary file preview is not available.
        </Text>
      </Center>
    );
  }

  return (
    <>
      {blob.isTruncated && (
        <Alert color="yellow" variant="light" m="md">
          This file is larger than the preview limit, so only the first 1 MB is
          shown.
        </Alert>
      )}
      <ScrollArea type="auto">
        <Box className="repo-code-view">
          {blob.content.split('\n').map((line, index) => (
            <div className="repo-code-line" key={index}>
              <span className="repo-code-line-number">{index + 1}</span>
              <code className="repo-code-line-content">{line || ' '}</code>
            </div>
          ))}
        </Box>
      </ScrollArea>
    </>
  );
}

function BlamePreview({
  repoId,
  refName,
  path: repoPath,
}: {
  repoId: string;
  refName: string;
  path: string;
}) {
  const { data } = useSuspenseQuery(
    repoBlameQueryOptions(repoId, refName, repoPath),
  );

  if (data.lines.length === 0) {
    return (
      <Center py="xl">
        <Text size="sm" c="dimmed">
          No blame data is available for this file.
        </Text>
      </Center>
    );
  }

  return (
    <>
      {data.isTruncated && (
        <Alert color="yellow" variant="light" m="md">
          This blame view is larger than the preview limit, so only the first
          5,000 lines are shown.
        </Alert>
      )}
      <ScrollArea type="auto">
        <Box className="repo-blame-view">
          {data.lines.map((line) => (
            <div className="repo-blame-line" key={line.lineNo}>
              <div className="repo-blame-meta">
                <Code>{shortCommitHash(line.commitHash)}</Code>
                <Text size="xs" truncate>
                  {line.authorName || line.authorEmail || 'Unknown author'}
                </Text>
                <Text size="xs" c="dimmed" truncate>
                  {line.authoredAt ? formatTimeAgo(line.authoredAt) : '—'}
                </Text>
              </div>
              <span className="repo-code-line-number">{line.lineNo}</span>
              <code className="repo-code-line-content">
                {line.content || ' '}
              </code>
            </div>
          ))}
        </Box>
      </ScrollArea>
    </>
  );
}

function ImageBlobPreview({
  blob,
  repoId,
  refName,
}: {
  blob: GetBlobResponse;
  repoId: string;
  refName: string;
}) {
  const [objectUrl, setObjectUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    let nextObjectUrl: string | null = null;

    setObjectUrl(null);
    setError(null);

    fetchRawBlob(repoId, refName, blob.path, controller.signal)
      .then((rawBlob) => {
        nextObjectUrl = URL.createObjectURL(rawBlob);
        if (!active) {
          URL.revokeObjectURL(nextObjectUrl);
          return;
        }
        setObjectUrl(nextObjectUrl);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted || !active) return;
        setError(
          cause instanceof Error ? cause.message : 'Failed to load image',
        );
      });

    return () => {
      active = false;
      controller.abort();
      if (nextObjectUrl) URL.revokeObjectURL(nextObjectUrl);
    };
  }, [blob.path, refName, repoId]);

  if (error) {
    return (
      <Center py="xl">
        <Text size="sm" c="dimmed">
          {error}
        </Text>
      </Center>
    );
  }

  if (!objectUrl) {
    return (
      <Center py="xl">
        <Loader size="sm" />
      </Center>
    );
  }

  return (
    <Center p="lg" bg="gray.0">
      <Image
        src={objectUrl}
        alt={blob.name}
        fit="contain"
        mah={720}
        maw="100%"
        onError={() => setError('Image preview is not available.')}
      />
    </Center>
  );
}

function ReadmePreview({ readme }: { readme: RepositoryReadme }) {
  return (
    <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
      <Group gap="xs" px="md" py="sm">
        <IconBook size={16} />
        <Text size="sm" fw={600}>
          {readme.name}
        </Text>
      </Group>
      {readme.isTruncated && (
        <Alert color="yellow" variant="light" mx="md" mb="md">
          This README is larger than the preview limit, so only the first 1 MB
          is shown.
        </Alert>
      )}
      <Box className="repo-markdown-body" px="md" pb="lg">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>
          {readme.content}
        </ReactMarkdown>
      </Box>
    </Paper>
  );
}

function CommitsTab({
  repoId,
  defaultBranch,
}: {
  repoId: string;
  defaultBranch: string;
}) {
  const search = Route.useSearch();
  const updateSearch = useRepoDetailSearchUpdater();
  const { refs, options, preferredRef } = useRepositoryRefOptions(
    repoId,
    defaultBranch,
  );
  const selectedRef =
    refs.some((ref) => ref.refName === search.ref) ||
    isCommitHashLike(search.ref)
      ? (search.ref ?? '')
      : preferredRef;
  const selectOptions = useMemo(
    () => selectOptionsWithCurrentRef(options, selectedRef),
    [options, selectedRef],
  );
  const pathFilter = search.historyPath ?? '';

  useEffect(() => {
    if (!preferredRef) return;
    if (
      !selectedRef ||
      (!refs.some((ref) => ref.refName === selectedRef) &&
        !isCommitHashLike(selectedRef))
    ) {
      updateSearch({
        tab: 'commits',
        ref: preferredRef,
        path: undefined,
        file: undefined,
        commit: undefined,
      });
    }
  }, [preferredRef, refs, selectedRef, updateSearch]);

  if (refs.length === 0 || !selectedRef) {
    return (
      <Text size="sm" c="dimmed">
        No active branches or tags are available for commit history.
      </Text>
    );
  }

  return (
    <Stack gap="md">
      <Select
        data={selectOptions}
        value={selectedRef}
        onChange={(value) =>
          updateSearch({
            tab: 'commits',
            ref: value ?? undefined,
            historyPath: pathFilter || undefined,
            file: undefined,
            commit: undefined,
          })
        }
        searchable
        allowDeselect={false}
        leftSection={<IconGitBranch size={14} />}
        w={{ base: '100%', sm: 280 }}
      />
      {pathFilter && (
        <Alert variant="light" color="blue" icon={<IconHistory size={16} />}>
          <Group justify="space-between" align="center">
            <Text size="sm">
              Showing commits that touched <Code>{pathFilter}</Code>
            </Text>
            <Button
              variant="subtle"
              size="xs"
              onClick={() =>
                updateSearch({
                  tab: 'commits',
                  ref: selectedRef,
                  historyPath: undefined,
                  commit: undefined,
                })
              }
            >
              Clear
            </Button>
          </Group>
        </Alert>
      )}
      <Suspense fallback={<TabFallback />}>
        <CommitList
          repoId={repoId}
          refName={selectedRef}
          pathFilter={pathFilter}
        />
      </Suspense>
    </Stack>
  );
}

function CompareTab({
  repoId,
  defaultBranch,
}: {
  repoId: string;
  defaultBranch: string;
}) {
  const search = Route.useSearch();
  const updateSearch = useRepoDetailSearchUpdater();
  const { refs, options, preferredRef } = useRepositoryRefOptions(
    repoId,
    defaultBranch,
  );
  const fallbackHeadRef =
    refs.find((ref) => ref.refName !== preferredRef)?.refName ?? preferredRef;
  const isAvailableRef = useCallback(
    (value: string | undefined) =>
      Boolean(
        value &&
        (refs.some((ref) => ref.refName === value) || isCommitHashLike(value)),
      ),
    [refs],
  );
  const baseRef = isAvailableRef(search.baseRef)
    ? (search.baseRef ?? '')
    : preferredRef;
  const headRef = isAvailableRef(search.headRef)
    ? (search.headRef ?? '')
    : fallbackHeadRef;
  const baseOptions = useMemo(
    () => selectOptionsWithCurrentRef(options, baseRef),
    [baseRef, options],
  );
  const headOptions = useMemo(
    () => selectOptionsWithCurrentRef(options, headRef),
    [headRef, options],
  );

  useEffect(() => {
    if (!preferredRef || !baseRef || !headRef) return;
    if (search.baseRef === baseRef && search.headRef === headRef) return;
    updateSearch({
      tab: 'compare',
      baseRef,
      headRef,
      path: undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
      historyPath: undefined,
    });
  }, [
    baseRef,
    headRef,
    preferredRef,
    search.baseRef,
    search.headRef,
    updateSearch,
  ]);

  if (refs.length === 0 || !baseRef || !headRef) {
    return (
      <Text size="sm" c="dimmed">
        No active branches or tags are available for comparison.
      </Text>
    );
  }

  return (
    <Stack gap="md">
      <Group align="flex-end">
        <Select
          label="Base"
          data={baseOptions}
          value={baseRef}
          onChange={(value) =>
            updateSearch({
              tab: 'compare',
              baseRef: value ?? undefined,
              headRef,
              path: undefined,
              file: undefined,
              view: undefined,
              commit: undefined,
              historyPath: undefined,
            })
          }
          searchable
          allowDeselect={false}
          leftSection={<IconGitBranch size={14} />}
          w={{ base: '100%', sm: 280 }}
        />
        <ActionIcon
          variant="default"
          size="lg"
          onClick={() =>
            updateSearch({
              tab: 'compare',
              baseRef: headRef,
              headRef: baseRef,
              path: undefined,
              file: undefined,
              view: undefined,
              commit: undefined,
              historyPath: undefined,
            })
          }
          aria-label="Swap base and head"
          title="Swap base and head"
        >
          <IconArrowsExchange size={16} />
        </ActionIcon>
        <Select
          label="Head"
          data={headOptions}
          value={headRef}
          onChange={(value) =>
            updateSearch({
              tab: 'compare',
              baseRef,
              headRef: value ?? undefined,
              path: undefined,
              file: undefined,
              view: undefined,
              commit: undefined,
              historyPath: undefined,
            })
          }
          searchable
          allowDeselect={false}
          leftSection={<IconGitBranch size={14} />}
          w={{ base: '100%', sm: 280 }}
        />
      </Group>

      <Suspense fallback={<TabFallback />}>
        <CompareContent
          repoId={repoId}
          baseRefName={baseRef}
          headRefName={headRef}
        />
      </Suspense>
    </Stack>
  );
}

function CompareContent({
  repoId,
  baseRefName,
  headRefName,
}: {
  repoId: string;
  baseRefName: string;
  headRefName: string;
}) {
  const updateSearch = useRepoDetailSearchUpdater();
  const { data } = useSuspenseQuery(
    repoCompareQueryOptions(repoId, baseRefName, headRefName),
  );

  return (
    <Stack gap="md">
      <Paper withBorder p="md" radius="sm">
        <Group justify="space-between" align="flex-start">
          <Stack gap={4}>
            <Text size="sm" fw={600}>
              {refDisplayName(data.headRefName)} compared to{' '}
              {refDisplayName(data.baseRefName)}
            </Text>
            <Group gap="xs">
              <Code>{shortCommitHash(data.baseCommitHash)}</Code>
              <Text size="xs" c="dimmed">
                to
              </Text>
              <Code>{shortCommitHash(data.headCommitHash)}</Code>
              {data.mergeBaseHash && (
                <Text size="xs" c="dimmed">
                  merge base <Code>{shortCommitHash(data.mergeBaseHash)}</Code>
                </Text>
              )}
            </Group>
          </Stack>
          <Group gap="xs">
            <Badge variant="light" color="green">
              {data.aheadCount.toLocaleString()} ahead
            </Badge>
            <Badge variant="light" color="gray">
              {data.behindCount.toLocaleString()} behind
            </Badge>
            <Badge variant="light" color="green">
              +{data.additions.toLocaleString()}
            </Badge>
            <Badge variant="light" color="red">
              -{data.deletions.toLocaleString()}
            </Badge>
            {data.isTruncated && (
              <Badge variant="light" color="yellow">
                Truncated
              </Badge>
            )}
          </Group>
        </Group>
      </Paper>

      <Stack gap="xs">
        <Group justify="space-between">
          <Text size="sm" fw={600}>
            Commits
          </Text>
          <Text size="xs" c="dimmed">
            Showing up to 50 commits
          </Text>
        </Group>
        {data.commits.length === 0 ? (
          <Text size="sm" c="dimmed">
            No commits are ahead of the base ref.
          </Text>
        ) : (
          <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
            <Table highlightOnHover>
              <Table.Tbody>
                {data.commits.map((commit) => (
                  <CommitRow
                    key={commit.hash}
                    commit={commit}
                    onSelect={() =>
                      updateSearch({
                        tab: 'commits',
                        ref: data.headRefName,
                        path: undefined,
                        file: undefined,
                        view: undefined,
                        commit: commit.hash,
                        historyPath: undefined,
                      })
                    }
                  />
                ))}
              </Table.Tbody>
            </Table>
          </Paper>
        )}
      </Stack>

      <Stack gap="xs">
        <Text size="sm" fw={600}>
          Files changed
        </Text>
        {data.files.length === 0 ? (
          <Text size="sm" c="dimmed">
            No file changes are available for this comparison.
          </Text>
        ) : (
          <Stack gap="md">
            {data.files.map((file) => (
              <CommitFileDiffView
                key={`${file.oldPath}:${file.newPath}`}
                file={file}
                commitHash={data.headCommitHash}
              />
            ))}
          </Stack>
        )}
      </Stack>
    </Stack>
  );
}

function CommitList({
  repoId,
  refName,
  pathFilter,
}: {
  repoId: string;
  refName: string;
  pathFilter: string;
}) {
  const search = Route.useSearch();
  const updateSearch = useRepoDetailSearchUpdater();
  const selectedCommitHash = search.commit ?? null;
  const { data, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useSuspenseInfiniteQuery(
      repoCommitsQueryOptions(repoId, refName, pathFilter),
    );

  const { ref: sentinelRef, entry } = useIntersection({ threshold: 0 });

  useEffect(() => {
    if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
      fetchNextPage();
    }
  }, [entry?.isIntersecting, hasNextPage, isFetchingNextPage, fetchNextPage]);

  const commits = data.pages.flatMap((page) => page.commits);

  if (commits.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        {pathFilter
          ? 'No commits are available for this path.'
          : 'No commits are available for this ref.'}
      </Text>
    );
  }

  return (
    <>
      <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
        <Table highlightOnHover>
          <Table.Tbody>
            {commits.map((commit) => (
              <CommitRow
                key={commit.hash}
                commit={commit}
                onSelect={() =>
                  updateSearch({
                    tab: 'commits',
                    ref: refName,
                    historyPath: pathFilter || undefined,
                    file: undefined,
                    commit: commit.hash,
                  })
                }
              />
            ))}
          </Table.Tbody>
        </Table>
      </Paper>
      <div ref={sentinelRef} style={{ height: 1 }} />
      {isFetchingNextPage && (
        <Center mt="md">
          <Loader size="sm" />
        </Center>
      )}
      <Drawer
        opened={selectedCommitHash !== null}
        onClose={() =>
          updateSearch({
            tab: 'commits',
            ref: refName,
            historyPath: pathFilter || undefined,
            commit: undefined,
          })
        }
        title={
          <Group gap="xs">
            <IconGitCommit size={16} />
            <Text fw={600}>
              {selectedCommitHash && shortCommitHash(selectedCommitHash)}
            </Text>
          </Group>
        }
        position="right"
        size="xl"
      >
        {selectedCommitHash && (
          <Suspense fallback={<TabFallback />}>
            <CommitDetails repoId={repoId} commitHash={selectedCommitHash} />
          </Suspense>
        )}
      </Drawer>
    </>
  );
}

function CommitRow({
  commit,
  onSelect,
}: {
  commit: RepositoryCommit;
  onSelect: () => void;
}) {
  const message = truncateMessage(commit.message || '(no commit message)', 96);

  return (
    <Table.Tr style={{ cursor: 'pointer' }} onClick={onSelect}>
      <Table.Td>
        <Box>
          <Text size="sm" fw={500} lineClamp={1}>
            {message}
          </Text>
          <Text size="xs" c="dimmed">
            {[
              commit.authorName || 'Unknown author',
              commit.committedAt && formatTimeAgo(commit.committedAt),
            ]
              .filter(Boolean)
              .join(', ')}
          </Text>
        </Box>
      </Table.Td>
      <Table.Td style={{ width: 1 }}>
        <Code>{shortCommitHash(commit.hash)}</Code>
      </Table.Td>
    </Table.Tr>
  );
}

function CommitDetails({
  repoId,
  commitHash,
}: {
  repoId: string;
  commitHash: string;
}) {
  const { data } = useSuspenseQuery(repoCommitQueryOptions(repoId, commitHash));
  const commit = data.commit;

  if (!commit) {
    return (
      <Text size="sm" c="dimmed">
        Commit details are not available.
      </Text>
    );
  }

  return (
    <Stack gap="md">
      <Paper withBorder p="md" radius="sm">
        <Stack gap="xs">
          <Text size="sm" fw={600}>
            {commit.message.split('\n')[0] || '(no commit message)'}
          </Text>
          {commit.message.split('\n').slice(1).join('\n').trim() && (
            <Text size="sm" c="dimmed" style={{ whiteSpace: 'pre-wrap' }}>
              {commit.message.split('\n').slice(1).join('\n').trim()}
            </Text>
          )}
          <Group gap="sm">
            <Text size="xs" c="dimmed">
              {commit.authorName || 'Unknown author'}
            </Text>
            <Text size="xs" c="dimmed">
              {formatTime(commit.committedAt)}
            </Text>
            <Code>{shortCommitHash(commit.hash)}</Code>
          </Group>
          {commit.parentHashes.length > 0 && (
            <Group gap={6}>
              <Text size="xs" c="dimmed">
                Parents
              </Text>
              {commit.parentHashes.map((hash) => (
                <Code key={hash}>{shortCommitHash(hash)}</Code>
              ))}
            </Group>
          )}
        </Stack>
      </Paper>

      <Group gap="sm">
        <Badge variant="light" color="green">
          +{data.additions.toLocaleString()}
        </Badge>
        <Badge variant="light" color="red">
          -{data.deletions.toLocaleString()}
        </Badge>
        {data.isTruncated && (
          <Badge variant="light" color="yellow">
            Truncated
          </Badge>
        )}
      </Group>

      {data.files.length === 0 ? (
        <Text size="sm" c="dimmed">
          This commit has no file diff to display.
        </Text>
      ) : (
        <Stack gap="md">
          {data.files.map((file) => (
            <CommitFileDiffView
              key={`${file.oldPath}:${file.newPath}`}
              file={file}
              commitHash={commit.hash}
            />
          ))}
        </Stack>
      )}
    </Stack>
  );
}

function CommitFileDiffView({
  file,
  commitHash,
}: {
  file: CommitFileDiff;
  commitHash: string;
}) {
  const updateSearch = useRepoDetailSearchUpdater();
  const viewablePath = file.status === 'deleted' ? '' : file.newPath;

  return (
    <Paper withBorder radius="sm" style={{ overflow: 'hidden' }}>
      <Group justify="space-between" px="md" py="sm">
        <Group gap="xs" miw={0}>
          <Badge variant="light" color={diffStatusColor(file.status)} size="sm">
            {file.status}
          </Badge>
          {viewablePath ? (
            <Anchor
              component="button"
              type="button"
              size="sm"
              fw={500}
              onClick={() =>
                updateSearch({
                  tab: 'code',
                  ref: commitHash,
                  path: undefined,
                  file: viewablePath,
                  view: undefined,
                  commit: undefined,
                  historyPath: undefined,
                })
              }
            >
              {viewablePath}
            </Anchor>
          ) : (
            <Text size="sm" fw={500} truncate>
              {file.oldPath}
            </Text>
          )}
        </Group>
        <Group gap={6}>
          <Text size="xs" c="green">
            +{file.additions}
          </Text>
          <Text size="xs" c="red">
            -{file.deletions}
          </Text>
        </Group>
      </Group>
      {file.isBinary ? (
        <Center py="lg">
          <Text size="sm" c="dimmed">
            Binary file diff is not available.
          </Text>
        </Center>
      ) : (
        <ScrollArea type="auto">
          <Box className="repo-diff-view">
            {file.patch.split('\n').map((line, index) => (
              <div className={diffLineClassName(line)} key={index}>
                <code>{line || ' '}</code>
              </div>
            ))}
          </Box>
        </ScrollArea>
      )}
      {file.isTruncated && (
        <Alert color="yellow" variant="light" m="md">
          This file diff is larger than the preview limit.
        </Alert>
      )}
    </Paper>
  );
}

function diffStatusColor(status: string) {
  switch (status) {
    case 'added':
      return 'green';
    case 'deleted':
      return 'red';
    case 'renamed':
    case 'copied':
      return 'violet';
    default:
      return 'blue';
  }
}

function diffLineClassName(line: string) {
  if (line.startsWith('+') && !line.startsWith('+++')) {
    return 'repo-diff-line repo-diff-line-add';
  }
  if (line.startsWith('-') && !line.startsWith('---')) {
    return 'repo-diff-line repo-diff-line-delete';
  }
  if (
    line.startsWith('diff --git') ||
    line.startsWith('index ') ||
    line.startsWith('@@') ||
    line.startsWith('---') ||
    line.startsWith('+++')
  ) {
    return 'repo-diff-line repo-diff-line-meta';
  }
  return 'repo-diff-line';
}

function RepoDetailPage() {
  const { repoId } = Route.useParams();
  const search = Route.useSearch();
  const updateSearch = useRepoDetailSearchUpdater();
  const { data } = useSuspenseQuery(repoDetailQueryOptions(repoId));
  const { data: configData } = useQuery(configQueryOptions);
  const activeTab = search.tab ?? 'code';

  const { data: branchesData } = useQuery({
    ...repoRefsQueryOptions(repoId, 'head'),
    enabled: false,
  });
  const { data: tagsData } = useQuery({
    ...repoRefsQueryOptions(repoId, 'tag'),
    enabled: false,
  });

  const repo = data.repository!;
  const source = (configData?.config?.sources ?? []).find(
    (item) => item.id === repo.sourceId,
  );
  const sourceValue = source ? sourcePrimaryLabel(source) : repo.sourceId;
  const sourceDescription = source ? sourceSecondaryLabel(source) : null;

  const meta = repo.meta as Record<string, unknown> | undefined;
  const ownerMeta = meta?.['owner'] as Record<string, unknown> | undefined;
  const avatarUrl = (ownerMeta?.['avatar_url'] as string) ?? '';
  const language = (meta?.['language'] as string) ?? '';
  const stars = (meta?.['stargazers_count'] as number) ?? 0;
  const forks = (meta?.['forks_count'] as number) ?? 0;
  const [owner, repoName] = repo.fullName.split('/');

  return (
    <Container fluid py="xl" px="xl">
      <Breadcrumbs mb="lg">
        <Link to="/repos" style={{ textDecoration: 'none', color: 'inherit' }}>
          <Text size="sm" c="dimmed">
            Repositories
          </Text>
        </Link>
        <Text size="sm">{repo.fullName}</Text>
      </Breadcrumbs>

      <Group align="flex-start" gap="lg" mb="xl">
        <Avatar src={avatarUrl} alt={owner} size="xl" radius="md" />
        <div style={{ flex: 1 }}>
          <Group gap="sm" align="center">
            <Title order={2}>{repoName}</Title>
            {repo.isArchived && (
              <Badge variant="light" color="yellow" size="sm">
                Archived
              </Badge>
            )}
            {repo.isPrivate && (
              <Badge variant="light" color="gray" size="sm">
                Private
              </Badge>
            )}
            {repo.isFork && (
              <Badge variant="light" color="grape" size="sm">
                Fork
              </Badge>
            )}
          </Group>
          <Text c="dimmed" size="sm">
            {owner}
          </Text>
          {repo.description && (
            <Text mt="xs" size="sm">
              {repo.description}
            </Text>
          )}
          <Group gap="lg" mt="sm">
            {language && (
              <Text size="xs" c="dimmed">
                {language}
              </Text>
            )}
            {stars > 0 && (
              <Text size="xs" c="dimmed">
                {stars.toLocaleString()} stars
              </Text>
            )}
            {forks > 0 && (
              <Text size="xs" c="dimmed">
                {forks.toLocaleString()} forks
              </Text>
            )}
            {repo.htmlUrl && (
              <Anchor
                href={repo.htmlUrl}
                target="_blank"
                rel="noopener noreferrer"
                size="xs"
                style={{ display: 'flex', alignItems: 'center', gap: 4 }}
              >
                GitHub <IconExternalLink size={12} />
              </Anchor>
            )}
          </Group>
        </div>
      </Group>

      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="xl">
        <InfoCard
          label="Source"
          value={sourceValue}
          description={sourceDescription ?? undefined}
        />
        <InfoCard label="Default Branch" value={repo.defaultBranch || '—'} />
        <InfoCard label="First Seen" value={formatTimeAgo(repo.createdAt)} />
        <InfoCard label="Last Seen" value={formatTimeAgo(repo.lastSeenAt)} />
      </SimpleGrid>

      <Tabs
        value={activeTab}
        onChange={(value) => {
          if (!value || !REPO_DETAIL_TABS.has(value as RepoDetailTab)) return;
          updateSearch(tabSearchPatch(value as RepoDetailTab));
        }}
      >
        <Tabs.List>
          <Tabs.Tab value="code" leftSection={<IconCode size={14} />}>
            Code
          </Tabs.Tab>
          <Tabs.Tab value="commits" leftSection={<IconGitCommit size={14} />}>
            Commits
          </Tabs.Tab>
          <Tabs.Tab value="compare" leftSection={<IconGitCompare size={14} />}>
            Compare
          </Tabs.Tab>
          <Tabs.Tab value="branches" leftSection={<IconGitBranch size={14} />}>
            Branches
            {branchesData ? ` (${branchesData.refs.length})` : ''}
          </Tabs.Tab>
          <Tabs.Tab value="tags" leftSection={<IconTag size={14} />}>
            Tags
            {tagsData ? ` (${tagsData.refs.length})` : ''}
          </Tabs.Tab>
          <Tabs.Tab value="changes" leftSection={<IconHistory size={14} />}>
            Changes
          </Tabs.Tab>
          <Tabs.Tab value="download" leftSection={<IconDownload size={14} />}>
            Download
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="code" pt="md">
          {activeTab === 'code' && (
            <Suspense fallback={<TabFallback />}>
              <CodeTab repoId={repoId} defaultBranch={repo.defaultBranch} />
            </Suspense>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="commits" pt="md">
          {activeTab === 'commits' && (
            <Suspense fallback={<TabFallback />}>
              <CommitsTab repoId={repoId} defaultBranch={repo.defaultBranch} />
            </Suspense>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="compare" pt="md">
          {activeTab === 'compare' && (
            <Suspense fallback={<TabFallback />}>
              <CompareTab repoId={repoId} defaultBranch={repo.defaultBranch} />
            </Suspense>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="branches" pt="md">
          {activeTab === 'branches' && (
            <Suspense fallback={<TabFallback />}>
              <RefsTab
                repoId={repoId}
                refKind="head"
                emptyLabel="No branches tracked yet."
              />
            </Suspense>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="tags" pt="md">
          {activeTab === 'tags' && (
            <Suspense fallback={<TabFallback />}>
              <RefsTab
                repoId={repoId}
                refKind="tag"
                emptyLabel="No tags tracked yet."
              />
            </Suspense>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="changes" pt="md">
          {activeTab === 'changes' && (
            <Suspense fallback={<TabFallback />}>
              <ChangesTab repoId={repoId} />
            </Suspense>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="download" pt="md">
          {activeTab === 'download' && (
            <DownloadTab
              repoId={repoId}
              archiveRepoSizeBytes={repo.archiveRepoSizeBytes}
            />
          )}
        </Tabs.Panel>
      </Tabs>
    </Container>
  );
}

function stageToStep(stage: DownloadStage): number {
  switch (stage) {
    case DownloadStage.COPY_BARE:
      return 0;
    case DownloadStage.MATERIALIZE_REFS:
      return 1;
    case DownloadStage.PACKAGE_ZIP:
      return 2;
    case DownloadStage.READY:
      return 3;
    default:
      return 0;
  }
}

function DownloadTab({
  repoId,
  archiveRepoSizeBytes,
}: {
  repoId: string;
  archiveRepoSizeBytes: bigint | undefined;
}) {
  const [downloadEvent, setDownloadEvent] =
    useState<StreamRepositoryDownloadResponse | null>(null);
  const [isPreparing, setIsPreparing] = useState(false);
  const [lastCompletedAt, setLastCompletedAt] = useState<Date | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, []);

  const handleCancel = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setIsPreparing(false);
    setDownloadEvent(null);
  };

  const handlePrepareDownload = async () => {
    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;
    setIsPreparing(true);
    setDownloadEvent(null);

    try {
      for await (const event of repoClient.streamRepositoryDownload(
        { repoId: BigInt(repoId) },
        { signal: controller.signal },
      )) {
        setDownloadEvent(event);

        if (event.state === DownloadState.READY) {
          await triggerRepositoryDownload(
            repoId,
            event.downloadId,
            event.downloadFilename,
          );
          setLastCompletedAt(new Date());
          toast.success('Repository download started');
          break;
        }

        if (event.state === DownloadState.FAILED) {
          toast.error(event.errorMessage || event.summary || 'Download failed');
          break;
        }
      }
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }

      const message =
        error instanceof Error
          ? error.message
          : 'Failed to prepare repository download';
      setDownloadEvent((current) => ({
        $typeName: 'gitplus.repo.v1.StreamRepositoryDownloadResponse',
        repoId: BigInt(repoId),
        state: DownloadState.FAILED,
        stage: current?.stage ?? DownloadStage.UNSPECIFIED,
        summary: current?.summary || 'Failed to prepare download',
        progressPercent: current?.progressPercent ?? 0,
        estimatedProcessingLabel: current?.estimatedProcessingLabel || '',
        estimatedProcessingBytes:
          current?.estimatedProcessingBytes ?? archiveRepoSizeBytes ?? 0n,
        estimatedDownloadBytes:
          current?.estimatedDownloadBytes ??
          estimatedDownloadSize(archiveRepoSizeBytes) ??
          0n,
        archiveSizeBytes:
          current?.archiveSizeBytes ?? archiveRepoSizeBytes ?? 0n,
        downloadId: current?.downloadId || '',
        downloadFilename: current?.downloadFilename || '',
        errorMessage: message,
      }));
      toast.error(message);
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
      }
      setIsPreparing(false);
    }
  };

  const currentState = downloadEvent?.state ?? DownloadState.UNSPECIFIED;
  const currentStage = downloadEvent?.stage ?? DownloadStage.UNSPECIFIED;
  const progressPercent = downloadEvent?.progressPercent ?? 0;
  const isRunning = isPreparing || currentState === DownloadState.RUNNING;
  const isFailed = currentState === DownloadState.FAILED;
  const isReady = currentState === DownloadState.READY;
  const processingSize =
    downloadEvent?.estimatedProcessingBytes || archiveRepoSizeBytes;
  const downloadSize =
    downloadEvent?.estimatedDownloadBytes ??
    estimatedDownloadSize(archiveRepoSizeBytes);
  const processingTime =
    downloadEvent?.estimatedProcessingLabel ||
    estimateProcessingTime(archiveRepoSizeBytes);

  return (
    <Stack gap="lg">
      <Text size="sm" c="dimmed">
        Download a zipped repository snapshot with the current active branches
        and tags materialized into a normal Git repository.
      </Text>

      <SimpleGrid cols={{ base: 1, sm: 3 }}>
        <Paper withBorder p="md" radius="sm">
          <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
            Repo Size
          </Text>
          <Text size="sm" fw={500} mt={4}>
            {formatEstimatedBytes(processingSize)}
          </Text>
        </Paper>
        <Paper withBorder p="md" radius="sm">
          <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
            Download Size (est.)
          </Text>
          <Text size="sm" fw={500} mt={4}>
            {formatEstimatedBytes(downloadSize)}
          </Text>
        </Paper>
        <Paper withBorder p="md" radius="sm">
          <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
            Processing Time (est.)
          </Text>
          <Text size="sm" fw={500} mt={4}>
            {processingTime}
          </Text>
        </Paper>
      </SimpleGrid>

      {isRunning && (
        <Paper withBorder p="lg" radius="sm">
          <Stepper active={stageToStep(currentStage)} size="sm" iconSize={28}>
            <Stepper.Step
              label="Copy"
              description="Clone bare repo"
              icon={<IconPackage size={14} />}
            />
            <Stepper.Step
              label="Restore"
              description="Refs & branches"
              icon={<IconGitBranch size={14} />}
            />
            <Stepper.Step
              label="Package"
              description="Zip archive"
              icon={<IconFileZip size={14} />}
            />
            <Stepper.Step
              label="Ready"
              description="Download file"
              icon={<IconCheck size={14} />}
            />
          </Stepper>

          <Progress
            value={progressPercent}
            radius="xl"
            size="sm"
            mt="lg"
            animated
          />

          <Group justify="space-between" mt="xs">
            <Text size="xs" c="dimmed">
              {downloadEvent?.summary || downloadStageLabel(currentStage)}
            </Text>
            <Text size="xs" c="dimmed">
              {progressPercent}%
            </Text>
          </Group>

          <Group justify="center" mt="md">
            <Button
              variant="subtle"
              color="gray"
              size="xs"
              leftSection={<IconX size={14} />}
              onClick={handleCancel}
            >
              Cancel
            </Button>
          </Group>
        </Paper>
      )}

      {isFailed && (
        <Alert
          variant="light"
          color="red"
          title="Download failed"
          icon={<IconAlertTriangle size={18} />}
        >
          <Text size="sm">
            {downloadEvent?.errorMessage || 'An unexpected error occurred.'}
          </Text>
          <Button
            size="xs"
            variant="light"
            color="red"
            mt="sm"
            leftSection={<IconCircleX size={14} />}
            onClick={handlePrepareDownload}
          >
            Retry
          </Button>
        </Alert>
      )}

      {isReady && lastCompletedAt && (
        <Alert variant="light" color="teal" icon={<IconCheck size={18} />}>
          <Group justify="space-between" align="center">
            <div>
              <Text size="sm" fw={500}>
                Download started
              </Text>
              <Text size="xs" c="dimmed">
                {downloadEvent?.downloadFilename} &middot;{' '}
                {dayjs(lastCompletedAt).fromNow()}
              </Text>
            </div>
            <Button
              variant="light"
              color="teal"
              size="xs"
              leftSection={<IconDownload size={14} />}
              onClick={handlePrepareDownload}
            >
              Download again
            </Button>
          </Group>
        </Alert>
      )}

      {!isRunning && (
        <div>
          <Button
            leftSection={<IconDownload size={16} />}
            onClick={handlePrepareDownload}
          >
            {isFailed ? 'Retry download' : 'Prepare & download'}
          </Button>
          {lastCompletedAt && !isReady && (
            <Text size="xs" c="dimmed" mt="xs">
              Last download {dayjs(lastCompletedAt).fromNow()}
            </Text>
          )}
        </div>
      )}
    </Stack>
  );
}

function RefsTab({
  repoId,
  refKind,
  emptyLabel,
}: {
  repoId: string;
  refKind: 'head' | 'tag';
  emptyLabel: string;
}) {
  const [showDeleted, setShowDeleted] = useState(false);
  const [historyRefName, setHistoryRefName] = useState<string | null>(null);
  const updateSearch = useRepoDetailSearchUpdater();
  const { data } = useSuspenseQuery(
    repoRefsQueryOptions(repoId, refKind, showDeleted),
  );
  const refs = data.refs;

  const browseCodeAtRef = (refName: string) => {
    updateSearch({
      tab: 'code',
      ref: refName,
      path: undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
      historyPath: undefined,
    });
  };

  const browseCommitsAtRef = (refName: string) => {
    updateSearch({
      tab: 'commits',
      ref: refName,
      path: undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
      historyPath: undefined,
    });
  };

  const compareRef = (refName: string) => {
    updateSearch({
      tab: 'compare',
      baseRef: undefined,
      headRef: refName,
      path: undefined,
      file: undefined,
      view: undefined,
      commit: undefined,
      historyPath: undefined,
    });
  };

  if (refs.length === 0 && !showDeleted) {
    return (
      <Text size="sm" c="dimmed">
        {emptyLabel}
      </Text>
    );
  }

  return (
    <>
      <Table striped highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Name</Table.Th>
            <Table.Th>Hash</Table.Th>
            <Table.Th>Last Commit</Table.Th>
            {showDeleted && <Table.Th>Status</Table.Th>}
            <Table.Th>Updated</Table.Th>
            <Table.Th style={{ width: 1 }} />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {refs.map((r) => (
            <RefRow
              key={r.id.toString()}
              ref_={r}
              showStatus={showDeleted}
              onBrowseCode={() => browseCodeAtRef(r.refName)}
              onBrowseCommits={() => browseCommitsAtRef(r.refName)}
              onCompare={() => compareRef(r.refName)}
              onHistory={() => setHistoryRefName(r.refName)}
            />
          ))}
        </Table.Tbody>
      </Table>
      <Checkbox
        mt="md"
        label="Show deleted"
        checked={showDeleted}
        onChange={(e) => setShowDeleted(e.currentTarget.checked)}
      />
      <Drawer
        opened={historyRefName !== null}
        onClose={() => setHistoryRefName(null)}
        title={
          <Group gap="xs">
            <IconHistory size={16} />
            <Text fw={600}>
              {historyRefName && stripRefPrefix(historyRefName)}
            </Text>
          </Group>
        }
        position="right"
        size="lg"
      >
        {historyRefName && (
          <Suspense fallback={<TabFallback />}>
            <RefHistoryDrawerContent repoId={repoId} refName={historyRefName} />
          </Suspense>
        )}
      </Drawer>
    </>
  );
}

function RefHistoryDrawerContent({
  repoId,
  refName,
}: {
  repoId: string;
  refName: string;
}) {
  const { data, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useSuspenseInfiniteQuery(repoRefChangesQueryOptions(repoId, refName));

  const { ref: sentinelRef, entry } = useIntersection({ threshold: 0 });

  useEffect(() => {
    if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
      fetchNextPage();
    }
  }, [entry?.isIntersecting, hasNextPage, isFetchingNextPage, fetchNextPage]);

  const changes = data.pages.flatMap((p) => p.changes);

  if (changes.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No changes recorded for this ref.
      </Text>
    );
  }

  return (
    <>
      <Table striped highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Time</Table.Th>
            <Table.Th>Action</Table.Th>
            <Table.Th>Commit</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {changes.map((c) => (
            <RefChangeRow key={c.id.toString()} change={c} />
          ))}
        </Table.Tbody>
      </Table>
      <div ref={sentinelRef} style={{ height: 1 }} />
      {isFetchingNextPage && (
        <Center mt="md">
          <Loader size="sm" />
        </Center>
      )}
    </>
  );
}

function RefChangeRow({ change }: { change: RepoRefChange }) {
  return (
    <Table.Tr>
      <Table.Td>
        <Text size="xs">{formatTime(change.createdAt)}</Text>
      </Table.Td>
      <Table.Td>
        <Badge
          variant="light"
          color={actionBadgeColor(change.action)}
          size="sm"
        >
          {change.action}
        </Badge>
      </Table.Td>
      <ChangeCommitCell change={change} />
    </Table.Tr>
  );
}

function ChangesTab({ repoId }: { repoId: string }) {
  const { data, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useSuspenseInfiniteQuery(repoRefChangesQueryOptions(repoId));

  const { ref: sentinelRef, entry } = useIntersection({ threshold: 0 });

  useEffect(() => {
    if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
      fetchNextPage();
    }
  }, [entry?.isIntersecting, hasNextPage, isFetchingNextPage, fetchNextPage]);

  const changes = data.pages.flatMap((p) => p.changes);

  if (changes.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No ref changes recorded yet.
      </Text>
    );
  }

  return (
    <>
      <Table striped highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Time</Table.Th>
            <Table.Th>Ref</Table.Th>
            <Table.Th>Action</Table.Th>
            <Table.Th>Commit</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {changes.map((c) => (
            <ChangeRow key={c.id.toString()} change={c} />
          ))}
        </Table.Tbody>
      </Table>
      <div ref={sentinelRef} style={{ height: 1 }} />
      {isFetchingNextPage && (
        <Center mt="md">
          <Loader size="sm" />
        </Center>
      )}
    </>
  );
}

function InfoCard({
  label,
  value,
  description,
}: {
  label: string;
  value: string;
  description?: string;
}) {
  return (
    <Box>
      <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
        {label}
      </Text>
      <Text size="sm" fw={500} mt={2}>
        {value}
      </Text>
      {description && (
        <Text size="xs" c="dimmed" mt={2}>
          {description}
        </Text>
      )}
    </Box>
  );
}

function RefRow({
  ref_,
  showStatus,
  onBrowseCode,
  onBrowseCommits,
  onCompare,
  onHistory,
}: {
  ref_: RepoRef;
  showStatus: boolean;
  onBrowseCode: () => void;
  onBrowseCommits: () => void;
  onCompare: () => void;
  onHistory: () => void;
}) {
  const commit = ref_.currentCommit;
  const isActive = ref_.status === 'active';

  return (
    <Table.Tr>
      <Table.Td>
        <Code>{stripRefPrefix(ref_.refName)}</Code>
      </Table.Td>
      <Table.Td>
        <Code>{shortHash(ref_.currentHash)}</Code>
      </Table.Td>
      <Table.Td style={{ maxWidth: 360 }}>
        {commit?.message ? (
          <>
            <Text size="xs" lineClamp={1}>
              {truncateMessage(commit.message)}
            </Text>
            {(commit.authorName || commit.committedAt) && (
              <Text size="xs" c="dimmed">
                {[
                  commit.authorName,
                  commit.committedAt && formatTimeAgo(commit.committedAt),
                ]
                  .filter(Boolean)
                  .join(', ')}
              </Text>
            )}
          </>
        ) : (
          <Text size="xs" c="dimmed">
            —
          </Text>
        )}
      </Table.Td>
      {showStatus && (
        <Table.Td>
          <Badge
            variant="light"
            color={ref_.status === 'active' ? 'green' : 'red'}
            size="sm"
          >
            {ref_.status}
          </Badge>
        </Table.Td>
      )}
      <Table.Td>
        <Text size="xs" c="dimmed">
          {formatTimeAgo(ref_.lastHashUpdatedAt ?? ref_.lastSeenAt)}
        </Text>
      </Table.Td>
      <Table.Td>
        <Group gap={4} wrap="nowrap">
          <ActionIcon
            variant="subtle"
            size="sm"
            onClick={onBrowseCode}
            disabled={!isActive}
            aria-label="Browse code"
            title="Browse code"
          >
            <IconCode size={14} />
          </ActionIcon>
          <ActionIcon
            variant="subtle"
            size="sm"
            onClick={onBrowseCommits}
            disabled={!isActive}
            aria-label="View commits"
            title="View commits"
          >
            <IconGitCommit size={14} />
          </ActionIcon>
          <ActionIcon
            variant="subtle"
            size="sm"
            onClick={onCompare}
            disabled={!isActive}
            aria-label="Compare ref"
            title="Compare ref"
          >
            <IconGitCompare size={14} />
          </ActionIcon>
          <ActionIcon
            variant="subtle"
            size="sm"
            onClick={onHistory}
            aria-label="View ref history"
            title="View ref history"
          >
            <IconHistory size={14} />
          </ActionIcon>
        </Group>
      </Table.Td>
    </Table.Tr>
  );
}

function ChangeRow({ change }: { change: RepoRefChange }) {
  return (
    <Table.Tr>
      <Table.Td>
        <Text size="xs">{formatTime(change.createdAt)}</Text>
      </Table.Td>
      <Table.Td>
        <Group gap={6}>
          {change.refKind === 'tag' ? (
            <IconTag size={14} />
          ) : (
            <IconGitBranch size={14} />
          )}
          <Code>{stripRefPrefix(change.refName)}</Code>
        </Group>
      </Table.Td>
      <Table.Td>
        <Badge
          variant="light"
          color={actionBadgeColor(change.action)}
          size="sm"
        >
          {change.action}
        </Badge>
      </Table.Td>
      <ChangeCommitCell change={change} maxWidth={320} />
    </Table.Tr>
  );
}
