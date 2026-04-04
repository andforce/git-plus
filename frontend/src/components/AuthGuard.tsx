import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Box,
  Button,
  Center,
  Paper,
  PasswordInput,
  Stack,
  Text,
  Title,
} from '@mantine/core';
import { IconLock } from '@tabler/icons-react';
import { queryClient } from '../router';
import { onAuthRequired, setToken } from '~lib/auth';
import { configClient } from '~lib/connect/client';

/**
 * Shared login overlay UI.
 * Used by both the root errorComponent (initial load) and AuthGuard (mid-session 401).
 */
export function AuthOverlay({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const timer = setTimeout(() => inputRef.current?.focus(), 100);
    return () => clearTimeout(timer);
  }, []);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!password.trim()) return;

      setLoading(true);
      setError('');
      setToken(password.trim());

      try {
        await configClient.ping({});
        onSuccess();
      } catch {
        setError('Invalid password');
      } finally {
        setLoading(false);
      }
    },
    [password, onSuccess],
  );

  return (
    <Box
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        backdropFilter: 'blur(12px)',
        WebkitBackdropFilter: 'blur(12px)',
        backgroundColor: 'rgba(0, 0, 0, 0.25)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Center h="100%">
        <Paper
          shadow="xl"
          radius="lg"
          p="xl"
          w={360}
          style={{
            backgroundColor: 'var(--mantine-color-body)',
          }}
        >
          <form onSubmit={handleSubmit}>
            <Stack gap="lg" align="center">
              <IconLock
                size={40}
                stroke={1.5}
                color="var(--mantine-color-dimmed)"
              />
              <div style={{ textAlign: 'center' }}>
                <Title order={3}>Git Plus</Title>
                <Text size="sm" c="dimmed" mt={4}>
                  Enter password to continue
                </Text>
              </div>
              <PasswordInput
                ref={inputRef}
                w="100%"
                placeholder="Password"
                value={password}
                onChange={(e) => setPassword(e.currentTarget.value)}
                error={error || undefined}
              />
              <Button
                type="submit"
                fullWidth
                loading={loading}
                disabled={!password.trim()}
              >
                Unlock
              </Button>
            </Stack>
          </form>
        </Paper>
      </Center>
    </Box>
  );
}

/**
 * Mid-session auth guard.
 * Renders inside the root layout to handle 401 responses after initial auth.
 * On 401, the transport clears the token and triggers this overlay.
 */
export function AuthGuard() {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    onAuthRequired(() => setVisible(true));
  }, []);

  if (!visible) return null;

  return (
    <AuthOverlay
      onSuccess={() => {
        setVisible(false);
        queryClient.invalidateQueries();
      }}
    />
  );
}
