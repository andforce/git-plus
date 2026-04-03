import {
  ActionIcon,
  Badge,
  Card,
  Group,
  Menu,
  Stack,
  Text,
} from '@mantine/core';
import {
  IconBrandGithub,
  IconDotsVertical,
  IconKey,
  IconPencil,
  IconTrash,
} from '@tabler/icons-react';
import type { Source } from '~rpc/gitplus/config/v1/config_pb';

interface SourceCardProps {
  source: Source;
  onEdit: () => void;
  onReplaceToken: () => void;
  onDelete: () => void;
}

export function SourceCard({
  source,
  onEdit,
  onReplaceToken,
  onDelete,
}: SourceCardProps) {
  const hasInclude = source.onlyIncludeRepos.length > 0;
  const hasExclude = source.excludeRepos.length > 0;

  const repoSummary = getRepoSummary(source);

  return (
    <Card withBorder radius="md" padding="md">
      <Group justify="space-between" wrap="nowrap" mb="sm">
        <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
          <IconBrandGithub size={18} style={{ flexShrink: 0 }} />
          <Text fw={600} size="sm" truncate="end">
            {source.id}
          </Text>
        </Group>

        <Menu position="bottom-end" withArrow shadow="md">
          <Menu.Target>
            <ActionIcon variant="subtle" color="gray" size="sm">
              <IconDotsVertical size={16} />
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Item leftSection={<IconPencil size={14} />} onClick={onEdit}>
              Edit
            </Menu.Item>
            <Menu.Item
              leftSection={<IconKey size={14} />}
              onClick={onReplaceToken}
            >
              Replace Token
            </Menu.Item>
            <Menu.Divider />
            <Menu.Item
              leftSection={<IconTrash size={14} />}
              color="red"
              onClick={onDelete}
            >
              Delete
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>
      </Group>

      <Stack gap={6}>
        <Text size="sm" c="dimmed">
          @{source.username}
        </Text>

        <Group gap={6} wrap="wrap">
          <Text size="xs" c="dimmed">
            {repoSummary.label}
          </Text>
          {hasInclude &&
            source.onlyIncludeRepos.slice(0, 2).map((repo) => (
              <Badge
                key={repo}
                size="xs"
                variant="outline"
                color="gray"
                fw={400}
              >
                {repo}
              </Badge>
            ))}
          {hasExclude &&
            !hasInclude &&
            source.excludeRepos.slice(0, 2).map((repo) => (
              <Badge
                key={repo}
                size="xs"
                variant="outline"
                color="gray"
                fw={400}
              >
                {repo}
              </Badge>
            ))}
          {repoSummary.extra > 0 && (
            <Text size="xs" c="dimmed">
              +{repoSummary.extra}
            </Text>
          )}
        </Group>
      </Stack>
    </Card>
  );
}

function getRepoSummary(source: Source) {
  const hasInclude = source.onlyIncludeRepos.length > 0;
  const hasExclude = source.excludeRepos.length > 0;

  if (!hasInclude && !hasExclude) {
    return { label: 'All repositories', extra: 0 };
  }

  if (hasInclude) {
    const total = source.onlyIncludeRepos.length;
    return {
      label: `${total} included ·`,
      extra: Math.max(0, total - 2),
    };
  }

  const total = source.excludeRepos.length;
  return {
    label: `${total} excluded ·`,
    extra: Math.max(0, total - 2),
  };
}
