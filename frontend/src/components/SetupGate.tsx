import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { SetupState } from '~lib/setup';
import { setToken } from '~lib/auth';
import { completeSetup, getSetupState, selectSetupDataDir } from '~lib/setup';

export function SetupGate({ children }: { children: React.ReactNode }) {
  const setupQuery = useQuery({
    queryKey: ['setup'],
    queryFn: getSetupState,
    retry: false,
  });

  if (setupQuery.isPending) {
    return <SetupShell title="Loading Git Plus..." />;
  }

  if (setupQuery.isError) {
    return (
      <SetupShell
        title="Git Plus could not start"
        description={
          setupQuery.error instanceof Error
            ? setupQuery.error.message
            : 'Setup state could not be loaded.'
        }
      />
    );
  }

  if (setupQuery.data.requiresSetup) {
    return (
      <SetupScreen
        setup={setupQuery.data}
        onSuccess={() => setupQuery.refetch()}
      />
    );
  }

  return children;
}

function SetupScreen({
  setup,
  onSuccess,
}: {
  setup: SetupState;
  onSuccess: () => void;
}) {
  const [currentSetup, setCurrentSetup] = useState(setup);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const passwordRequired = !currentSetup.authConfigured;

  useEffect(() => {
    setCurrentSetup(setup);
  }, [setup]);

  const setupMutation = useMutation({
    mutationFn: () =>
      completeSetup({
        password: password.trim() || undefined,
      }),
    onSuccess: () => {
      if (password.trim()) {
        setToken(password.trim());
      }
      onSuccess();
    },
    onError: (mutationError) =>
      setError(
        mutationError instanceof Error
          ? mutationError.message
          : 'Setup failed.',
      ),
  });

  const dataDirMutation = useMutation({
    mutationFn: selectSetupDataDir,
    onSuccess: (nextSetup) => {
      if (nextSetup) {
        setCurrentSetup(nextSetup);
        if (!nextSetup.requiresSetup) {
          onSuccess();
        }
      }
    },
    onError: (mutationError) =>
      setError(
        mutationError instanceof Error
          ? mutationError.message
          : 'Data directory selection failed.',
      ),
  });

  return (
    <SetupShell title="Set up Git Plus">
      <form
        style={{ display: 'grid', gap: 18 }}
        onSubmit={(event) => {
          event.preventDefault();
          setError('');
          setupMutation.mutate();
        }}
      >
        <div
          style={{
            border: '1px solid #d0d7de',
            borderRadius: 8,
            padding: 14,
            background: '#f6f8fa',
          }}
        >
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>
            Data location
          </div>
          <div
            style={{
              color: '#57606a',
              fontFamily:
                'ui-monospace, SFMono-Regular, SFMono, Consolas, monospace',
              fontSize: 12,
              overflowWrap: 'anywhere',
            }}
          >
            {currentSetup.dataDir}
          </div>
          <button
            type="button"
            onClick={() => {
              setError('');
              dataDirMutation.mutate();
            }}
            disabled={dataDirMutation.isPending}
            style={secondaryButtonStyle}
          >
            Choose folder
          </button>
        </div>

        <label style={{ display: 'grid', gap: 6, fontSize: 13 }}>
          <span style={{ fontWeight: 600 }}>Dashboard password</span>
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.currentTarget.value)}
            required={passwordRequired}
            autoFocus={passwordRequired}
            placeholder={
              passwordRequired
                ? 'At least 8 characters'
                : 'Leave blank to keep current password'
            }
            style={inputStyle}
          />
        </label>

        {!currentSetup.encryptionConfigured && (
          <div style={{ color: '#57606a', fontSize: 13 }}>
            A local token encryption key will be generated.
          </div>
        )}

        {error && <div style={{ color: '#cf222e', fontSize: 13 }}>{error}</div>}

        <button
          type="submit"
          disabled={setupMutation.isPending}
          style={primaryButtonStyle}
        >
          Continue
        </button>
      </form>
    </SetupShell>
  );
}

function SetupShell({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: React.ReactNode;
}) {
  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        background: '#f6f8fa',
        color: '#1f2328',
        padding: 16,
      }}
    >
      <div
        style={{
          width: 'min(100%, 560px)',
          border: '1px solid #d0d7de',
          borderRadius: 10,
          background: '#fff',
          boxShadow: '0 8px 24px rgba(140,149,159,0.2)',
        }}
      >
        <div style={{ borderBottom: '1px solid #d0d7de', padding: 22 }}>
          <h1 style={{ margin: 0, fontSize: 22, lineHeight: 1.2 }}>{title}</h1>
          <p style={{ margin: '8px 0 0', color: '#57606a', fontSize: 14 }}>
            {description ?? 'Create the local dashboard lock and storage keys.'}
          </p>
        </div>
        {children && <div style={{ padding: 22 }}>{children}</div>}
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  minHeight: 38,
  border: '1px solid #d0d7de',
  borderRadius: 6,
  padding: '0 10px',
  fontSize: 14,
};

const primaryButtonStyle: React.CSSProperties = {
  minHeight: 40,
  border: 0,
  borderRadius: 6,
  background: '#1f883d',
  color: '#fff',
  fontWeight: 600,
  cursor: 'pointer',
};

const secondaryButtonStyle: React.CSSProperties = {
  marginTop: 12,
  minHeight: 34,
  border: '1px solid #d0d7de',
  borderRadius: 6,
  background: '#fff',
  color: '#1f2328',
  fontWeight: 600,
  cursor: 'pointer',
  padding: '0 12px',
};
