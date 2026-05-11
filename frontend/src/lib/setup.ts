export type SetupState = {
  requiresSetup: boolean;
  authConfigured: boolean;
  encryptionConfigured: boolean;
  configExists: boolean;
  authMode: 'disabled' | 'environment' | 'local' | 'unset';
  dataDir: string;
};

export async function getSetupState(): Promise<SetupState> {
  const response = await fetch('/api/setup');
  if (!response.ok) {
    throw new Error(await setupErrorMessage(response));
  }
  return (await response.json()) as SetupState;
}

export async function completeSetup(input: {
  password?: string;
}): Promise<SetupState> {
  const response = await fetch('/api/setup', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await setupErrorMessage(response));
  }
  return (await response.json()) as SetupState;
}

export async function selectSetupDataDir(): Promise<SetupState | null> {
  const response = await fetch('/api/setup/select-data-dir', {
    method: 'POST',
  });
  if (response.status === 204) return null;
  if (!response.ok) {
    throw new Error(await setupErrorMessage(response));
  }
  return (await response.json()) as SetupState;
}

async function setupErrorMessage(response: Response): Promise<string> {
  const message = (await response.text()).trim();
  return message || `Setup request failed with ${response.status}`;
}
