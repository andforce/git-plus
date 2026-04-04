import { Link, createFileRoute } from '@tanstack/react-router';
import {
  Anchor,
  Avatar,
  Badge,
  Box,
  Breadcrumbs,
  Code,
  Container,
  Group,
  SimpleGrid,
  Table,
  Tabs,
  Text,
  Title,
} from '@mantine/core';
import { useSuspenseQuery } from '@tanstack/react-query';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import dayjs from 'dayjs';
import {
  IconExternalLink,
  IconGitBranch,
  IconHistory,
  IconTag,
} from '@tabler/icons-react';
import type { RepoRef, RepoRefChange } from '~rpc/gitplus/repo/v1/repo_pb';
import { repoDetailQueryOptions } from '~lib/repo-queries';

export const Route = createFileRoute('/_dashboard/repos/$repoId')({
  loader: ({ context: { queryClient }, params: { repoId } }) =>
    queryClient.ensureQueryData(repoDetailQueryOptions(repoId)),
  component: RepoDetailPage,
});

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

function RepoDetailPage() {
  const { repoId } = Route.useParams();
  const { data } = useSuspenseQuery(repoDetailQueryOptions(repoId));

  const repo = data.repository!;
  const refs = data.refs;
  const changes = data.recentChanges;

  const meta = repo.meta as Record<string, unknown> | undefined;
  const ownerMeta = meta?.['owner'] as Record<string, unknown> | undefined;
  const avatarUrl = (ownerMeta?.['avatar_url'] as string) ?? '';
  const language = (meta?.['language'] as string) ?? '';
  const stars = (meta?.['stargazers_count'] as number) ?? 0;
  const forks = (meta?.['forks_count'] as number) ?? 0;
  const [owner, repoName] = repo.fullName.split('/');

  const branches = refs.filter((r) => r.refKind === 'head');
  const tags = refs.filter((r) => r.refKind === 'tag');

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
        <InfoCard label="Source" value={repo.sourceId} />
        <InfoCard label="Default Branch" value={repo.defaultBranch || '—'} />
        <InfoCard label="First Seen" value={formatTimeAgo(repo.createdAt)} />
        <InfoCard label="Last Seen" value={formatTimeAgo(repo.lastSeenAt)} />
      </SimpleGrid>

      <Tabs defaultValue="branches">
        <Tabs.List>
          <Tabs.Tab value="branches" leftSection={<IconGitBranch size={14} />}>
            Branches ({branches.length})
          </Tabs.Tab>
          <Tabs.Tab value="tags" leftSection={<IconTag size={14} />}>
            Tags ({tags.length})
          </Tabs.Tab>
          <Tabs.Tab value="changes" leftSection={<IconHistory size={14} />}>
            Changes ({changes.length})
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="branches" pt="md">
          {branches.length === 0 ? (
            <Text size="sm" c="dimmed">
              No branches tracked yet.
            </Text>
          ) : (
            <Table striped highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Hash</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Last Seen</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {branches.map((r) => (
                  <RefRow key={r.id.toString()} ref_={r} />
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="tags" pt="md">
          {tags.length === 0 ? (
            <Text size="sm" c="dimmed">
              No tags tracked yet.
            </Text>
          ) : (
            <Table striped highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Hash</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Last Seen</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {tags.map((r) => (
                  <RefRow key={r.id.toString()} ref_={r} />
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        <Tabs.Panel value="changes" pt="md">
          {changes.length === 0 ? (
            <Text size="sm" c="dimmed">
              No ref changes recorded yet.
            </Text>
          ) : (
            <Table striped highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Time</Table.Th>
                  <Table.Th>Ref</Table.Th>
                  <Table.Th>Action</Table.Th>
                  <Table.Th>Hash</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {changes.map((c) => (
                  <ChangeRow key={c.id.toString()} change={c} />
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>
      </Tabs>
    </Container>
  );
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <Box>
      <Text size="xs" c="dimmed" tt="uppercase" fw={600}>
        {label}
      </Text>
      <Text size="sm" fw={500} mt={2}>
        {value}
      </Text>
    </Box>
  );
}

function RefRow({ ref_ }: { ref_: RepoRef }) {
  return (
    <Table.Tr>
      <Table.Td>
        <Code>{stripRefPrefix(ref_.refName)}</Code>
      </Table.Td>
      <Table.Td>
        <Code>{shortHash(ref_.currentHash)}</Code>
      </Table.Td>
      <Table.Td>
        <Badge
          variant="light"
          color={ref_.status === 'active' ? 'green' : 'red'}
          size="sm"
        >
          {ref_.status}
        </Badge>
      </Table.Td>
      <Table.Td>
        <Text size="xs" c="dimmed">
          {formatTimeAgo(ref_.lastSeenAt)}
        </Text>
      </Table.Td>
    </Table.Tr>
  );
}

function ChangeRow({ change }: { change: RepoRefChange }) {
  const hashDisplay = (() => {
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
  })();

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
      <Table.Td>{hashDisplay}</Table.Td>
    </Table.Tr>
  );
}
