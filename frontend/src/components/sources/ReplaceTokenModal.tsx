import {
  Alert,
  Button,
  Group,
  Modal,
  PasswordInput,
  Stack,
  Text,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { IconAlertCircle } from '@tabler/icons-react';
import { useEffect } from 'react';
import type { Source } from '~rpc/gitplus/config/v1/config_pb';

interface ReplaceTokenData {
  sourceId: string;
  tokenPlaintext: string;
}

interface ReplaceTokenModalProps {
  source?: Source | null;
  opened: boolean;
  onClose: () => void;
  onSubmit: (data: ReplaceTokenData) => void;
  loading?: boolean;
}

export function ReplaceTokenModal({
  source,
  opened,
  onClose,
  onSubmit,
  loading,
}: ReplaceTokenModalProps) {
  const form = useForm({
    initialValues: {
      tokenPlaintext: '',
    },
    validate: {
      tokenPlaintext: (value) =>
        value.trim() ? null : 'New token is required',
    },
  });

  useEffect(() => {
    if (opened) {
      form.reset();
    }
  }, [opened]);

  const handleSubmit = (values: { tokenPlaintext: string }) => {
    if (!source) return;
    onSubmit({
      sourceId: source.id,
      tokenPlaintext: values.tokenPlaintext.trim(),
    });
  };

  return (
    <Modal opened={opened} onClose={onClose} title="Replace Token" size="md">
      <form onSubmit={form.onSubmit(handleSubmit)}>
        <Stack gap="md">
          <Alert
            variant="light"
            color="orange"
            icon={<IconAlertCircle size={16} />}
          >
            <Text size="sm">
              You are replacing the token for source{' '}
              <Text component="span" fw={600}>
                {source?.id}
              </Text>
              . The new token will be encrypted before storage.
            </Text>
          </Alert>

          <PasswordInput
            label="New Token"
            description="The new personal access token"
            placeholder="ghp_xxxxxxxxxxxx"
            required
            {...form.getInputProps('tokenPlaintext')}
          />

          <Group justify="flex-end" mt="md">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={loading} color="orange">
              Replace Token
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
