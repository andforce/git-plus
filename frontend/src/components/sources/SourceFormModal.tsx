import {
  Button,
  Group,
  Modal,
  PasswordInput,
  Stack,
  TagsInput,
  Text,
  TextInput,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconInfoCircle } from '@tabler/icons-react';
import { useEffect } from 'react';
import type { Source } from '~rpc/gitplus/config/v1/config_pb';
import { Platform } from '~rpc/gitplus/config/v1/config_pb';

interface CreateData {
  source: {
    id: string;
    platform: Platform;
    username: string;
    tokenPlaintext: string;
    onlyIncludeRepos: Array<string>;
    excludeRepos: Array<string>;
  };
}

interface UpdateData {
  sourceId: string;
  patch: {
    id: string;
    platform: Platform;
    username: string;
    onlyIncludeRepos: { values: Array<string> };
    excludeRepos: { values: Array<string> };
  };
}

interface SourceFormModalProps {
  mode: 'create' | 'edit';
  source?: Source | null;
  opened: boolean;
  onClose: () => void;
  onSubmit: (data: CreateData | UpdateData) => void;
  loading?: boolean;
}

interface FormValues {
  id: string;
  username: string;
  tokenPlaintext: string;
  onlyIncludeRepos: Array<string>;
  excludeRepos: Array<string>;
}

export function SourceFormModal({
  mode,
  source,
  opened,
  onClose,
  onSubmit,
  loading,
}: SourceFormModalProps) {
  const form = useForm<FormValues>({
    initialValues: {
      id: '',
      username: '',
      tokenPlaintext: '',
      onlyIncludeRepos: [],
      excludeRepos: [],
    },
    validate: {
      id: (value) => (value.trim() ? null : 'Source ID is required'),
      username: (value) => (value.trim() ? null : 'Username is required'),
      tokenPlaintext: (value) =>
        mode === 'create' && !value.trim() ? 'Token is required' : null,
    },
  });

  useEffect(() => {
    if (opened && mode === 'edit' && source) {
      form.setValues({
        id: source.id,
        username: source.username,
        tokenPlaintext: '',
        onlyIncludeRepos: [...source.onlyIncludeRepos],
        excludeRepos: [...source.excludeRepos],
      });
      form.resetDirty();
    }
    if (opened && mode === 'create') {
      form.reset();
    }
  }, [opened, mode, source]);

  const handleSubmit = (values: FormValues) => {
    if (mode === 'create') {
      onSubmit({
        source: {
          id: values.id.trim(),
          platform: Platform.GITHUB,
          username: values.username.trim(),
          tokenPlaintext: values.tokenPlaintext.trim(),
          onlyIncludeRepos: values.onlyIncludeRepos,
          excludeRepos: values.excludeRepos,
        },
      });
    } else {
      onSubmit({
        sourceId: source!.id,
        patch: {
          id: values.id.trim(),
          platform: Platform.GITHUB,
          username: values.username.trim(),
          onlyIncludeRepos: { values: values.onlyIncludeRepos },
          excludeRepos: { values: values.excludeRepos },
        },
      });
    }
  };

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={mode === 'create' ? 'Add New Source' : 'Edit Source'}
      size="lg"
    >
      <form onSubmit={form.onSubmit(handleSubmit)}>
        <Stack gap="md">
          <TextInput
            label="Source ID"
            description="A unique identifier for this source"
            placeholder="my-github"
            required
            {...form.getInputProps('id')}
          />

          <TextInput
            label="Platform"
            value="GitHub"
            readOnly
            variant="filled"
          />

          <TextInput
            label="Username"
            description="Your username on this platform"
            placeholder="octocat"
            required
            {...form.getInputProps('username')}
          />

          {mode === 'create' ? (
            <PasswordInput
              label="Access Token"
              description="Personal access token for API authentication"
              placeholder="ghp_xxxxxxxxxxxx"
              required
              {...form.getInputProps('tokenPlaintext')}
            />
          ) : (
            <Group gap="xs" c="dimmed">
              <IconInfoCircle size={14} />
              <Text size="sm">
                Use &quot;Replace Token&quot; from the source menu to update the
                token.
              </Text>
            </Group>
          )}

          <TagsInput
            label="Only Include Repos"
            description="Only sync these repositories. Leave empty to include all."
            placeholder="Type a repo name and press Enter"
            {...form.getInputProps('onlyIncludeRepos')}
            clearable
          />

          <TagsInput
            label="Exclude Repos"
            description="Exclude these repositories from syncing"
            placeholder="Type a repo name and press Enter"
            {...form.getInputProps('excludeRepos')}
            clearable
          />

          <Group justify="flex-end" mt="md">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={loading}>
              {mode === 'create' ? 'Create Source' : 'Save Changes'}
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
