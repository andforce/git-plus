import { useState } from 'react';
import { createFileRoute } from '@tanstack/react-router';
import {
  ActionIcon,
  Anchor,
  Avatar,
  Box,
  Button,
  Card,
  Container,
  Flex,
  Group,
  Menu,
  Pagination,
  Select,
  SimpleGrid,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import {
  IconChevronDown,
  IconExternalLink,
  IconSearch,
  IconSortAscending,
  IconSortDescending,
  IconStar,
  IconX,
} from '@tabler/icons-react';
import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import type { Repository } from '~rpc/gitplus/repo/v1/repo_pb';
import { configQueryOptions } from '~lib/config-queries';
import { repoListQueryOptions } from '~lib/repo-queries';

const PAGE_SIZE = 30;

const SORT_OPTIONS: Array<{
  key: string;
  label: string;
  order: 'asc' | 'desc';
}> = [
  { key: 'created_at_desc', label: 'Found (Newest)', order: 'desc' },
  { key: 'created_at_asc', label: 'Found (Oldest)', order: 'asc' },
  { key: 'name_asc', label: 'Name (A-Z)', order: 'asc' },
  { key: 'name_desc', label: 'Name (Z-A)', order: 'desc' },
];

export const Route = createFileRoute('/_dashboard/repos')({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(configQueryOptions),
      queryClient.ensureQueryData(repoListQueryOptions(1, PAGE_SIZE, '', '')),
    ]),
  component: ReposPage,
});

const LANGUAGE_COLORS: Record<string, string> = {
  JavaScript: '#f1e05a',
  TypeScript: '#3178c6',
  Python: '#3572A5',
  Go: '#00ADD8',
  Rust: '#dea584',
  Java: '#b07219',
  'C++': '#f34b7d',
  C: '#555555',
  'C#': '#178600',
  Ruby: '#701516',
  PHP: '#4F5D95',
  Swift: '#F05138',
  Kotlin: '#A97BFF',
  Dart: '#00B4AB',
  Shell: '#89e051',
  Lua: '#000080',
  Vim: '#199f4b',
  HTML: '#e34c26',
  CSS: '#563d7c',
  SCSS: '#c6538c',
  Vue: '#41b883',
  Svelte: '#ff3e00',
  Zig: '#ec915c',
  Nix: '#7e7eff',
  Elixir: '#6e4a7e',
  Haskell: '#5e5086',
  Scala: '#c22d40',
  OCaml: '#3be133',
  Jupyter: '#DA5B0B',
  R: '#198CE7',
  Perl: '#0298c3',
};

function languageColor(lang: string): string {
  return LANGUAGE_COLORS[lang] ?? '#8b8b8b';
}

function ReposPage() {
  const { data: configData } = useSuspenseQuery(configQueryOptions);
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');
  const [sourceId, setSourceId] = useState('');
  const [sort, setSort] = useState('created_at_desc');
  const [debouncedSearch] = useDebouncedValue(search, 300);

  const { data } = useQuery(
    repoListQueryOptions(page, PAGE_SIZE, debouncedSearch, sourceId, sort),
  );

  const sources = configData.config?.sources ?? [];
  const sourceOptions = sources.map((s) => ({
    value: s.id,
    label: `${s.id} — @${s.username}`,
  }));

  const repos = data?.repositories ?? [];
  const totalCount = data?.totalCount ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalCount / PAGE_SIZE));

  const handleSearchChange = (value: string) => {
    setSearch(value);
    setPage(1);
  };

  const handleSourceChange = (value: string | null) => {
    setSourceId(value ?? '');
    setPage(1);
  };

  const handleSortChange = (key: string) => {
    setSort(key);
    setPage(1);
  };

  const currentSort =
    SORT_OPTIONS.find((o) => o.key === sort) ?? SORT_OPTIONS[0];

  return (
    <Container fluid py="xl" px="xl">
      <Group justify="space-between" align="flex-start" mb="xl">
        <div>
          <Title order={2}>Repositories</Title>
          <Text c="dimmed" size="sm">
            {totalCount} repositories synced from your sources
          </Text>
        </div>
      </Group>

      <Group gap="sm" mb="lg" wrap="nowrap">
        <TextInput
          placeholder="Search by name or description..."
          leftSection={<IconSearch size={16} />}
          rightSection={
            search ? (
              <ActionIcon
                variant="transparent"
                size="sm"
                onClick={() => handleSearchChange('')}
              >
                <IconX size={14} />
              </ActionIcon>
            ) : null
          }
          value={search}
          onChange={(e) => handleSearchChange(e.currentTarget.value)}
          style={{ flex: 1 }}
        />
        <Select
          placeholder="All sources"
          data={sourceOptions}
          value={sourceId || null}
          onChange={handleSourceChange}
          clearable
          w={240}
          style={{ flexShrink: 0 }}
        />
        <Menu shadow="md" width={180}>
          <Menu.Target>
            <Button
              variant="default"
              rightSection={<IconChevronDown size={14} />}
              leftSection={
                currentSort.order === 'asc' ? (
                  <IconSortAscending size={16} />
                ) : (
                  <IconSortDescending size={16} />
                )
              }
              style={{ flexShrink: 0 }}
            >
              {currentSort.label}
            </Button>
          </Menu.Target>
          <Menu.Dropdown>
            {SORT_OPTIONS.map((option) => (
              <Menu.Item
                key={option.key}
                onClick={() => handleSortChange(option.key)}
                leftSection={
                  option.order === 'asc' ? (
                    <IconSortAscending size={14} />
                  ) : (
                    <IconSortDescending size={14} />
                  )
                }
                style={{
                  backgroundColor:
                    sort === option.key
                      ? 'var(--mantine-color-blue-light)'
                      : undefined,
                }}
              >
                {option.label}
              </Menu.Item>
            ))}
          </Menu.Dropdown>
        </Menu>
      </Group>

      {repos.length === 0 ? (
        <Text size="sm" c="dimmed">
          {debouncedSearch || sourceId
            ? 'No repositories match the current filters.'
            : 'No repositories synced yet.'}
        </Text>
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
          {repos.map((repo) => (
            <RepoCard key={repo.id.toString()} repo={repo} />
          ))}
        </SimpleGrid>
      )}
      {totalPages > 1 && (
        <Pagination
          total={totalPages}
          value={page}
          onChange={setPage}
          size="sm"
          mt="md"
        />
      )}
    </Container>
  );
}

function RepoCard({ repo }: { repo: Repository }) {
  const meta = repo.meta as Record<string, unknown> | undefined;
  const language = (meta?.['language'] as string) ?? '';
  const stars = (meta?.['stargazers_count'] as number) ?? 0;
  const ownerMeta = meta?.['owner'] as Record<string, unknown> | undefined;
  const avatarUrl = (ownerMeta?.['avatar_url'] as string) ?? '';
  const [owner, repoName] = repo.fullName.split('/');

  return (
    <Card
      padding="lg"
      radius="md"
      withBorder
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        cursor: 'pointer',
      }}
      onClick={() => window.open(repo.htmlUrl || undefined, '_blank')}
    >
      <Flex gap="md" align="flex-start" mb="sm">
        <Avatar src={avatarUrl} alt={owner} size="md" radius="sm" />
        <Box flex={1} miw={0}>
          <Text fw={600} size="md" truncate>
            {repoName}
          </Text>
          <Text size="sm" c="dimmed">
            {owner}
          </Text>
        </Box>
      </Flex>

      <Text
        size="sm"
        c="dimmed"
        lh={1.6}
        mb="md"
        flex={1}
        lineClamp={3}
        style={{ wordBreak: 'break-word' }}
      >
        {repo.description || 'No description'}
      </Text>

      <Group
        justify="space-between"
        mt="auto"
        pt="sm"
        style={{ borderTop: '1px solid var(--mantine-color-default-border)' }}
      >
        <Group gap="md">
          {language && (
            <Group gap={6}>
              <Box
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  backgroundColor: languageColor(language),
                  flexShrink: 0,
                }}
              />
              <Text size="xs" c="dimmed">
                {language}
              </Text>
            </Group>
          )}
          {stars > 0 && (
            <Group gap={4}>
              <IconStar
                size={12}
                style={{ color: 'var(--mantine-color-dimmed)' }}
              />
              <Text size="xs" c="dimmed">
                {stars.toLocaleString()}
              </Text>
            </Group>
          )}
        </Group>

        <Anchor
          href={repo.htmlUrl || undefined}
          target="_blank"
          rel="noopener noreferrer"
          size="xs"
          c="dimmed"
          onClick={(e) => e.stopPropagation()}
          style={{ display: 'flex', alignItems: 'center', gap: 4 }}
        >
          GitHub <IconExternalLink size={12} />
        </Anchor>
      </Group>
    </Card>
  );
}
