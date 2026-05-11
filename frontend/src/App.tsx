import { timestampDate } from '@bufbuild/protobuf/wkt';
import { ConnectError } from '@connectrpc/connect';
import { QueryClient, useMutation, useQuery } from '@tanstack/react-query';
import {
  IconAlertTriangle,
  IconBook,
  IconBrandGithub,
  IconCalendarClock,
  IconCheck,
  IconChevronRight,
  IconCircleDot,
  IconDatabase,
  IconExternalLink,
  IconFolder,
  IconGitBranch,
  IconHelpCircle,
  IconLock,
  IconMenu2,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconSettings,
  IconShieldCheck,
  IconStar,
  IconTag,
  IconTool,
  IconX,
} from '@tabler/icons-react';
import dayjs from 'dayjs';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import type {
  RepoRef,
  RepoRefChange,
  Repository,
} from '~rpc/gitplus/repo/v1/repo_pb';
import type { Source } from '~rpc/gitplus/config/v1/config_pb';
import type { Task } from '~rpc/gitplus/task/v1/task_pb';
import type { SetupState } from '~lib/setup';
import { clearToken, getToken, setToken } from '~lib/auth';
import {
  configClient,
  cronClient,
  repoClient,
  taskClient,
} from '~lib/connect/client';
import { completeSetup, getSetupState, selectSetupDataDir } from '~lib/setup';
import { Platform } from '~rpc/gitplus/config/v1/config_pb';
import { TaskEnqueueResult, TaskState } from '~rpc/gitplus/task/v1/task_pb';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: 1,
    },
  },
});

const GITHUB_TOKEN_URL = 'https://github.com/settings/tokens/new';

type Route =
  | { name: 'repos'; search?: string }
  | { name: 'repo'; id: string }
  | { name: 'config' }
  | { name: 'sources' }
  | { name: 'cron' }
  | { name: 'tasks' }
  | { name: 'task'; id: string }
  | { name: 'not-found' };

type SourceFormState = {
  source?: Source;
  mode: 'create' | 'edit';
};

const navItems = [
  { label: 'Repositories', path: '/repos', icon: IconDatabase },
  { label: 'Configuration', path: '/config', icon: IconSettings },
  { label: 'Sources', path: '/config/sources', icon: IconBrandGithub },
  { label: 'Cron', path: '/config/cron', icon: IconCalendarClock },
  { label: 'Tasks', path: '/maintenance/tasks', icon: IconTool },
];

const sortOptions = [
  { value: 'created_at_desc', label: 'Recently found' },
  { value: 'created_at_asc', label: 'Oldest found' },
  { value: 'name_asc', label: 'Name A-Z' },
  { value: 'name_desc', label: 'Name Z-A' },
];

export function App() {
  const route = useRoute();
  const [mobileNav, setMobileNav] = useState(false);
  const [globalSearch, setGlobalSearch] = useState('');
  const setupQuery = useQuery({
    queryKey: ['setup'],
    queryFn: getSetupState,
    retry: false,
  });
  const setupReady = setupQuery.data?.requiresSetup === false;
  const authQuery = useQuery({
    queryKey: ['auth', getToken()],
    queryFn: () => configClient.ping({}),
    enabled: setupReady,
    retry: false,
  });
  const activeRepoSearch = route.name === 'repos' ? (route.search ?? '') : '';

  useEffect(() => {
    if (route.name === 'repos') {
      setGlobalSearch(activeRepoSearch);
    }
  }, [route.name, activeRepoSearch]);

  if (setupQuery.isPending) {
    return <LoadingScreen />;
  }

  if (setupQuery.isError) {
    return <StartupErrorScreen error={setupQuery.error} />;
  }

  if (setupQuery.data.requiresSetup) {
    return (
      <SetupScreen
        setup={setupQuery.data}
        onSuccess={() => setupQuery.refetch()}
      />
    );
  }

  if (authQuery.isError) {
    clearToken();
    return <LoginScreen onSuccess={() => authQuery.refetch()} />;
  }

  if (authQuery.isPending) {
    return <LoadingScreen />;
  }

  const submitGlobalSearch = (event: React.FormEvent) => {
    event.preventDefault();
    const trimmed = globalSearch.trim();
    navigate(trimmed ? `/repos?q=${encodeURIComponent(trimmed)}` : '/repos');
  };

  return (
    <div className="min-h-screen bg-[#f6f8fa] text-[#1f2328]">
      <FirstRunSourcePrompt />
      <header className="sticky top-0 z-30 border-b border-[#d0d7de] bg-[#24292f] text-white">
        <div className="flex h-14 items-center gap-3 px-3 sm:px-4">
          <button
            type="button"
            className="rounded-md p-2 text-white hover:bg-white/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da] lg:hidden"
            onClick={() => setMobileNav((value) => !value)}
            aria-label="Toggle navigation"
          >
            <IconMenu2 size={20} />
          </button>
          <button
            type="button"
            className="flex shrink-0 items-center gap-2 rounded-md px-2 py-1 text-sm font-semibold hover:bg-white/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]"
            onClick={() => navigate('/repos')}
          >
            <img src="/nav-icon.png" alt="" className="size-6" />
            Git Plus
          </button>
          <form
            className="hidden min-w-0 flex-1 items-center lg:flex"
            onSubmit={submitGlobalSearch}
          >
            <div className="flex h-8 w-full max-w-md items-center gap-2 rounded-md border border-white/20 bg-[#24292f] px-3 text-sm text-white/60 focus-within:border-white/40 focus-within:ring-2 focus-within:ring-white/10">
              <IconSearch size={16} />
              <input
                aria-label="Search repositories"
                className="h-full min-w-0 flex-1 bg-transparent text-sm text-white outline-none placeholder:text-white/60"
                value={globalSearch}
                onChange={(event) => setGlobalSearch(event.currentTarget.value)}
                placeholder="Search repositories..."
              />
            </div>
          </form>
          <button
            type="button"
            className="ml-auto rounded-md border border-white/20 px-3 py-1.5 text-sm font-medium hover:bg-white/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]"
            onClick={() => {
              clearToken();
              queryClient.clear();
              location.reload();
            }}
          >
            Lock
          </button>
        </div>
      </header>

      <div className="mx-auto grid max-w-[1440px] lg:grid-cols-[256px_minmax(0,1fr)]">
        <aside
          className={`${
            mobileNav ? 'block' : 'hidden'
          } border-b border-[#d0d7de] bg-white p-3 lg:sticky lg:top-14 lg:block lg:min-h-[calc(100vh-56px)] lg:border-b-0 lg:border-r`}
        >
          <nav className="space-y-1">
            {navItems.map((item) => (
              <NavButton
                key={item.path}
                active={isActiveNav(route, item.path)}
                icon={item.icon}
                label={item.label}
                onClick={() => {
                  navigate(item.path);
                  setMobileNav(false);
                }}
              />
            ))}
          </nav>
        </aside>

        <main className="min-w-0 px-4 py-6 sm:px-6 lg:px-8">
          <RouteView route={route} />
        </main>
      </div>
    </div>
  );
}

function RouteView({ route }: { route: Route }) {
  switch (route.name) {
    case 'repo':
      return <RepoDetailPage repoId={route.id} />;
    case 'config':
      return <ConfigPage />;
    case 'sources':
      return <SourcesPage />;
    case 'cron':
      return <CronPage />;
    case 'tasks':
      return <TasksPage />;
    case 'task':
      return <TaskDetailPage taskId={route.id} />;
    case 'repos':
      return <ReposPage initialSearch={route.search ?? ''} />;
    case 'not-found':
      return <NotFoundPage />;
  }
}

function SetupScreen({
  setup,
  onSuccess,
}: {
  setup: SetupState;
  onSuccess: () => void;
}) {
  const [currentSetup, setCurrentSetup] = useState(setup);
  const [password, setPasswordValue] = useState('');
  const [error, setError] = useState('');
  const passwordRequired = !currentSetup.authConfigured;

  useEffect(() => {
    setCurrentSetup(setup);
  }, [setup]);

  const mutation = useMutation({
    mutationFn: () =>
      completeSetup({
        password: password.trim() || undefined,
      }),
    onSuccess: () => {
      if (password.trim()) setToken(password.trim());
      queryClient.invalidateQueries();
      toast.success('Setup complete');
      onSuccess();
    },
    onError: (mutationError) => setError(errorMessage(mutationError)),
  });
  const selectDataDirMutation = useMutation({
    mutationFn: selectSetupDataDir,
    onSuccess: (nextSetup) => {
      if (!nextSetup) return;
      setCurrentSetup(nextSetup);
      queryClient.setQueryData(['setup'], nextSetup);
      toast.success('Data location updated');
      if (!nextSetup.requiresSetup) onSuccess();
    },
    onError: (mutationError) => setError(errorMessage(mutationError)),
  });

  return (
    <div className="flex min-h-screen items-center justify-center bg-[#f6f8fa] p-4">
      <form
        className="w-full max-w-xl rounded-lg border border-[#d0d7de] bg-white shadow-sm"
        onSubmit={(event) => {
          event.preventDefault();
          setError('');
          mutation.mutate();
        }}
      >
        <div className="border-b border-[#d0d7de] px-6 py-5">
          <div className="flex items-start gap-3">
            <div className="rounded-full border border-[#d0d7de] bg-[#f6f8fa] p-3 text-[#57606a]">
              <IconShieldCheck size={24} />
            </div>
            <div className="min-w-0">
              <h1 className="text-xl font-semibold">Set up Git Plus</h1>
              <p className="mt-1 text-sm text-[#57606a]">
                Create the local dashboard lock and storage keys.
              </p>
            </div>
          </div>
        </div>

        <div className="space-y-5 px-6 py-5">
          <div className="rounded-md border border-[#d0d7de] bg-[#f6f8fa] p-3">
            <div className="flex items-start gap-2">
              <IconFolder
                size={18}
                className="mt-0.5 shrink-0 text-[#57606a]"
              />
              <div className="min-w-0">
                <p className="text-sm font-medium">Data location</p>
                <p className="mt-1 break-all font-mono text-xs text-[#57606a]">
                  {currentSetup.dataDir}
                </p>
              </div>
            </div>
            <div className="mt-3 flex justify-end">
              <Button
                variant="secondary"
                loading={selectDataDirMutation.isPending}
                onClick={() => {
                  setError('');
                  selectDataDirMutation.mutate();
                }}
              >
                <IconFolder size={16} />
                Choose folder
              </Button>
            </div>
          </div>

          <Field label="Dashboard password">
            <Input
              type="password"
              value={password}
              onChange={setPasswordValue}
              autoFocus={passwordRequired}
              required={passwordRequired}
              placeholder={
                passwordRequired
                  ? 'At least 8 characters'
                  : 'Leave blank to keep current password'
              }
            />
          </Field>

          {!currentSetup.encryptionConfigured && (
            <StatusNote
              tone="neutral"
              title="A local token encryption key will be generated"
            />
          )}

          {error && <p className="text-sm text-[#cf222e]">{error}</p>}

          <Button
            type="submit"
            className="w-full justify-center"
            loading={mutation.isPending}
          >
            <IconCheck size={16} />
            Continue
          </Button>
        </div>
      </form>
    </div>
  );
}

function FirstRunSourcePrompt() {
  const [form, setForm] = useState<SourceFormState | null>(null);
  const [dismissed, setDismissed] = useState(
    () => sessionStorage.getItem('git-plus-first-source-dismissed') === 'true',
  );
  const configQuery = useQuery({
    queryKey: ['config'],
    queryFn: () => configClient.getConfig({}),
  });
  const sourceCount = configQuery.data?.config?.sources.length ?? 0;
  const needsSource =
    configQuery.data != null && (!configQuery.data.exists || sourceCount === 0);

  useEffect(() => {
    if (needsSource && !dismissed && !form) {
      setForm({ mode: 'create' });
    }
  }, [dismissed, form, needsSource]);

  if (!form) return null;

  return (
    <SourceForm
      form={form}
      onClose={() => {
        sessionStorage.setItem('git-plus-first-source-dismissed', 'true');
        setDismissed(true);
        setForm(null);
      }}
    />
  );
}

function ReposPage({ initialSearch = '' }: { initialSearch?: string }) {
  const [search, setSearch] = useState(initialSearch);
  const [sourceId, setSourceId] = useState('');
  const [sort, setSort] = useState('created_at_desc');
  const configQuery = useQuery({
    queryKey: ['config'],
    queryFn: () => configClient.getConfig({}),
  });
  const reposQuery = useQuery({
    queryKey: ['repos', search, sourceId, sort],
    queryFn: () =>
      repoClient.listRepositories({
        pageSize: 100,
        search,
        sourceId,
        sort,
      }),
  });
  const repos = reposQuery.data?.repositories ?? [];

  useEffect(() => {
    setSearch(initialSearch);
  }, [initialSearch]);

  return (
    <PageFrame
      title="Repositories"
      description={`${reposQuery.data?.totalCount ?? 0} repositories synced from configured sources`}
      actions={
        <Button
          onClick={() => queryClient.invalidateQueries({ queryKey: ['repos'] })}
        >
          <IconRefresh size={16} />
          Refresh
        </Button>
      }
    >
      <div className="mb-4 grid min-w-0 gap-2 md:grid-cols-[minmax(0,1fr)_220px_180px]">
        <SearchBox
          value={search}
          onChange={setSearch}
          placeholder="Find a repository..."
          ariaLabel="Filter repositories"
        />
        <Select
          value={sourceId}
          onChange={setSourceId}
          className="w-full"
          ariaLabel="Filter by source"
          options={[
            { value: '', label: 'All sources' },
            ...(configQuery.data?.config?.sources ?? []).map((source) => ({
              value: source.id,
              label: sourceLabel(source),
            })),
          ]}
        />
        <Select
          value={sort}
          onChange={setSort}
          options={sortOptions}
          className="w-full"
          ariaLabel="Sort repositories"
        />
      </div>

      <Panel>
        {reposQuery.isPending ? (
          <ListSkeleton />
        ) : repos.length === 0 ? (
          <EmptyState
            icon={<IconBook size={28} />}
            title="No repositories found"
            description="Run a source sync after adding a GitHub token."
            actionLabel="Open tasks"
            onAction={() => navigate('/maintenance/tasks')}
          />
        ) : (
          <div className="divide-y divide-[#d0d7de]">
            {repos.map((repo) => (
              <RepoListItem key={repo.id.toString()} repo={repo} />
            ))}
          </div>
        )}
      </Panel>
    </PageFrame>
  );
}

function RepoListItem({ repo }: { repo: Repository }) {
  const meta = repo.meta ?? {};
  const language = typeof meta.language === 'string' ? meta.language : '';
  const stars =
    typeof meta.stargazers_count === 'number' ? meta.stargazers_count : 0;

  return (
    <button
      type="button"
      className="flex w-full min-w-0 gap-4 px-4 py-4 text-left hover:bg-[#f6f8fa] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#0969da]"
      onClick={() => navigate(`/repos/${repo.id.toString()}`)}
    >
      <div className="mt-1 rounded-md border border-[#d0d7de] bg-[#f6f8fa] p-2 text-[#57606a]">
        <IconBook size={18} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-base font-semibold text-[#0969da]">
            {repo.fullName}
          </span>
          <Label>
            {repo.visibility || (repo.isPrivate ? 'private' : 'public')}
          </Label>
          {repo.status !== 'active' && (
            <Label tone="warning">{repo.status}</Label>
          )}
        </div>
        <p className="mt-1 line-clamp-2 text-sm text-[#57606a]">
          {repo.description || 'No description provided.'}
        </p>
        <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-[#57606a]">
          {language && (
            <span className="inline-flex items-center gap-1.5">
              <span className="size-3 rounded-full bg-[#f1e05a]" />
              {language}
            </span>
          )}
          <span className="inline-flex items-center gap-1">
            <IconStar size={14} />
            {stars.toLocaleString()}
          </span>
          <span>Last seen {formatRelative(repo.lastSeenAt)}</span>
        </div>
      </div>
      <IconChevronRight size={18} className="mt-2 shrink-0 text-[#57606a]" />
    </button>
  );
}

function RepoDetailPage({ repoId }: { repoId: string }) {
  const [refKind, setRefKind] = useState<'head' | 'tag'>('head');
  const repoQuery = useQuery({
    queryKey: ['repo', repoId],
    queryFn: () => repoClient.getRepository({ id: BigInt(repoId) }),
  });
  const refsQuery = useQuery({
    queryKey: ['repo', repoId, 'refs', refKind],
    queryFn: () =>
      repoClient.listRefs({
        repoId: BigInt(repoId),
        refKind,
        includeDeleted: true,
      }),
  });
  const changesQuery = useQuery({
    queryKey: ['repo', repoId, 'changes'],
    queryFn: () =>
      repoClient.listRefChanges({
        repoId: BigInt(repoId),
        pageSize: 50,
      }),
  });
  const repo = repoQuery.data?.repository;

  if (repoQuery.isPending) return <ListSkeleton />;
  if (!repo) return <NotFound />;

  return (
    <PageFrame
      title={repo.fullName}
      description={repo.description || 'No description provided.'}
      actions={
        repo.htmlUrl ? (
          <a
            href={repo.htmlUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-2 rounded-md border border-[#d0d7de] bg-white px-3 py-1.5 text-sm font-medium hover:bg-[#f6f8fa]"
          >
            <IconExternalLink size={16} />
            GitHub
          </a>
        ) : null
      }
    >
      <div className="mb-4 flex flex-wrap gap-2">
        <Label>{repo.visibility || 'public'}</Label>
        <Label tone={repo.status === 'active' ? 'success' : 'warning'}>
          {repo.status}
        </Label>
        <Label>Updated {formatRelative(repo.updatedAt)}</Label>
      </div>

      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(320px,380px)]">
        <Panel title="Refs">
          <Segmented
            value={refKind}
            onChange={(value) => setRefKind(value as 'head' | 'tag')}
            options={[
              { value: 'head', label: 'Branches', icon: IconGitBranch },
              { value: 'tag', label: 'Tags', icon: IconTag },
            ]}
          />
          <div className="mt-3 divide-y divide-[#d0d7de]">
            {(refsQuery.data?.refs ?? []).map((ref) => (
              <RefRow key={ref.id.toString()} refItem={ref} />
            ))}
          </div>
        </Panel>

        <Panel title="Recent changes">
          <div className="divide-y divide-[#d0d7de]">
            {(changesQuery.data?.changes ?? []).slice(0, 12).map((change) => (
              <ChangeRow key={change.id.toString()} change={change} />
            ))}
          </div>
        </Panel>
      </div>
    </PageFrame>
  );
}

function ConfigPage() {
  const configQuery = useQuery({
    queryKey: ['config'],
    queryFn: () => configClient.getConfig({}),
  });
  const checkQuery = useQuery({
    queryKey: ['config', 'check'],
    queryFn: () => configClient.checkConfig({}),
  });
  const config = configQuery.data?.config;
  const updateMutation = useMutation({
    mutationFn: (input: { concurrency: number; maxRetryTimes: number }) =>
      configClient.updateConfig(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] });
      toast.success('Configuration updated');
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const [concurrency, setConcurrency] = useState(5);
  const [maxRetryTimes, setMaxRetryTimes] = useState(2);

  useEffect(() => {
    if (!config) return;
    setConcurrency(config.concurrency);
    setMaxRetryTimes(config.maxRetryTimes);
  }, [config]);

  return (
    <PageFrame
      title="Configuration"
      description="Core runtime settings and validation state"
    >
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        <Panel title="Runtime settings" padded>
          <form
            className="grid gap-4 sm:grid-cols-2"
            onSubmit={(event) => {
              event.preventDefault();
              updateMutation.mutate({ concurrency, maxRetryTimes });
            }}
          >
            <Field label="Concurrency">
              <Input
                type="number"
                min={1}
                value={String(concurrency)}
                onChange={(value) => setConcurrency(Number(value))}
              />
            </Field>
            <Field label="Max retry times">
              <Input
                type="number"
                min={0}
                value={String(maxRetryTimes)}
                onChange={(value) => setMaxRetryTimes(Number(value))}
              />
            </Field>
            <div className="sm:col-span-2">
              <Button type="submit" loading={updateMutation.isPending}>
                <IconCheck size={16} />
                Save settings
              </Button>
            </div>
          </form>
        </Panel>

        <Panel title="Validation" padded>
          <div className="space-y-3">
            {(checkQuery.data?.issues ?? []).length === 0 ? (
              <StatusNote tone="success" title="Configuration looks ready" />
            ) : (
              checkQuery.data?.issues.map((issue) => (
                <StatusNote
                  key={`${issue.code}-${issue.sourceId}-${issue.message}`}
                  tone="danger"
                  title={issue.message}
                  detail={issue.code}
                />
              ))
            )}
          </div>
        </Panel>
      </div>
    </PageFrame>
  );
}

function SourcesPage() {
  const [form, setForm] = useState<SourceFormState | null>(null);
  const configQuery = useQuery({
    queryKey: ['config'],
    queryFn: () => configClient.getConfig({}),
  });
  const sources = configQuery.data?.config?.sources ?? [];
  const deleteMutation = useMutation({
    mutationFn: (sourceId: string) => configClient.deleteSource({ sourceId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] });
      toast.success('Source deleted');
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  return (
    <PageFrame
      title="Sources"
      description="GitHub accounts and repository selection rules"
      actions={
        <Button onClick={() => setForm({ mode: 'create' })}>
          <IconPlus size={16} />
          Add source
        </Button>
      }
    >
      {sources.length === 0 ? (
        <Panel>
          <EmptyState
            icon={<IconBrandGithub size={28} />}
            title="Connect your first GitHub source"
            description="Use a personal access token to sync owned, starred, or watched repositories."
            actionLabel="Add source"
            onAction={() => setForm({ mode: 'create' })}
          />
        </Panel>
      ) : (
        <div className="grid gap-3 xl:grid-cols-2">
          {sources.map((source) => (
            <SourceCard
              key={source.id}
              source={source}
              onEdit={() => setForm({ mode: 'edit', source })}
              onDelete={() => {
                if (confirm(`Delete ${sourceLabel(source)}?`)) {
                  deleteMutation.mutate(source.id);
                }
              }}
            />
          ))}
        </div>
      )}
      {form && <SourceForm form={form} onClose={() => setForm(null)} />}
    </PageFrame>
  );
}

function CronPage() {
  const runtimeQuery = useQuery({
    queryKey: ['cron'],
    queryFn: () => cronClient.getCronRuntime({}),
  });
  const [cron, setCron] = useState('');
  useEffect(
    () => setCron(runtimeQuery.data?.runtime?.cron ?? ''),
    [runtimeQuery.data],
  );
  const mutation = useMutation({
    mutationFn: () => cronClient.updateCron({ cron }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cron'] });
      queryClient.invalidateQueries({ queryKey: ['config'] });
      toast.success('Cron updated');
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  return (
    <PageFrame title="Cron" description="Schedule automatic full sync runs">
      <Panel title="Schedule" padded>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            mutation.mutate();
          }}
        >
          <Field label="Cron expression">
            <Input value={cron} placeholder="0 3 * * *" onChange={setCron} />
          </Field>
          <StatusNote
            tone={runtimeQuery.data?.runtime?.enabled ? 'success' : 'neutral'}
            title={
              runtimeQuery.data?.runtime?.enabled
                ? 'Scheduled sync enabled'
                : 'Scheduled sync disabled'
            }
            detail={runtimeQuery.data?.runtime?.lastError}
          />
          <Button type="submit" loading={mutation.isPending}>
            <IconCheck size={16} />
            Save cron
          </Button>
        </form>
      </Panel>
    </PageFrame>
  );
}

function TasksPage() {
  const runtimeQuery = useQuery({
    queryKey: ['task', 'runtime'],
    queryFn: () => taskClient.getTaskRuntime({}),
    refetchInterval: 3000,
  });
  const runsQuery = useQuery({
    queryKey: ['task', 'runs'],
    queryFn: () => taskClient.listTaskRuns({ pageSize: 30 }),
    refetchInterval: 5000,
  });
  const configQuery = useQuery({
    queryKey: ['config'],
    queryFn: () => configClient.getConfig({}),
  });
  const syncAll = useMutation({
    mutationFn: () => taskClient.enqueueFullSync({}),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['task'] });
      toast.success(`Sync all ${enqueueResultLabel(response.result)}`);
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const syncSource = useMutation({
    mutationFn: (sourceId: string) =>
      taskClient.enqueueSourceSync({ sourceId }),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['task'] });
      toast.success(`Source sync ${enqueueResultLabel(response.result)}`);
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const activeTasks = [
    ...(runtimeQuery.data?.runningTask ? [runtimeQuery.data.runningTask] : []),
    ...(runtimeQuery.data?.queuedTasks ?? []),
  ];

  return (
    <PageFrame
      title="Tasks"
      description="Run sync jobs and inspect background activity"
      actions={
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
          <Button
            onClick={() => syncAll.mutate()}
            loading={syncAll.isPending}
            className="justify-center"
          >
            <IconRefresh size={16} />
            Sync all
          </Button>
          <Select
            value=""
            onChange={(value) => value && syncSource.mutate(value)}
            className="w-full sm:w-[180px]"
            ariaLabel="Sync source"
            options={[
              { value: '', label: 'Sync source...' },
              ...(configQuery.data?.config?.sources ?? []).map((source) => ({
                value: source.id,
                label: sourceLabel(source),
              })),
            ]}
          />
        </div>
      }
    >
      {activeTasks.length > 0 && (
        <Panel title="Active">
          <div className="space-y-2">
            {activeTasks.map((task) => (
              <TaskRow key={task.taskId} task={task} />
            ))}
          </div>
        </Panel>
      )}
      <Panel title="History" className="mt-4">
        <div className="divide-y divide-[#d0d7de]">
          {(runsQuery.data?.taskRuns ?? []).map((task) => (
            <TaskRow key={task.taskId} task={task} />
          ))}
        </div>
      </Panel>
    </PageFrame>
  );
}

function TaskDetailPage({ taskId }: { taskId: string }) {
  const taskQuery = useQuery({
    queryKey: ['task', taskId],
    queryFn: () => taskClient.getTaskRun({ taskId }),
    refetchInterval: 5000,
  });
  const logsQuery = useQuery({
    queryKey: ['task', taskId, 'logs'],
    queryFn: () => taskClient.listTaskRunLogs({ taskId }),
    refetchInterval: 5000,
  });
  const task = taskQuery.data?.taskRun;

  return (
    <PageFrame title={task?.name ?? 'Task'} description={taskId}>
      {task && (
        <Panel title="Summary">
          <TaskRow task={task} compact />
        </Panel>
      )}
      <Panel title="Logs" className="mt-4">
        <div className="divide-y divide-[#d0d7de]">
          {(logsQuery.data?.logs ?? []).map((log) => (
            <div key={log.id.toString()} className="px-4 py-3 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs text-[#57606a]">
                  {formatAbsolute(log.createdAt)}
                </span>
                <Label>{log.eventType}</Label>
              </div>
              <p className="mt-1 text-[#1f2328]">
                {log.summary || log.errorMessage || 'No summary'}
              </p>
            </div>
          ))}
        </div>
      </Panel>
    </PageFrame>
  );
}

function SourceForm({
  form,
  onClose,
}: {
  form: SourceFormState;
  onClose: () => void;
}) {
  const source = form.source;
  const [username, setUsername] = useState(source?.username ?? '');
  const [token, setTokenValue] = useState('');
  const [includeDefaults, setIncludeDefaults] = useState(
    source?.includeDefaults ?? true,
  );
  const [includeStarred, setIncludeStarred] = useState(
    source?.includeStarred ?? false,
  );
  const [includeWatching, setIncludeWatching] = useState(
    source?.includeWatching ?? false,
  );
  const [onlyIncludeRepos, setOnlyIncludeRepos] = useState(
    source?.onlyIncludeRepos.join('\n') ?? '',
  );
  const [excludeRepos, setExcludeRepos] = useState(
    source?.excludeRepos.join('\n') ?? '',
  );
  const createMutation = useMutation({
    mutationFn: () =>
      configClient.createSource({
        source: {
          platform: Platform.GITHUB,
          name: '',
          username,
          tokenPlaintext: token,
          includeDefaults,
          includeStarred,
          includeWatching,
          onlyIncludeRepos: parseLines(onlyIncludeRepos),
          excludeRepos: parseLines(excludeRepos),
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] });
      toast.success('Source created');
      onClose();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const updateMutation = useMutation({
    mutationFn: async () => {
      if (!source) return;
      await configClient.updateSource({
        sourceId: source.id,
        patch: {
          platform: Platform.GITHUB,
          name: '',
          username,
          includeDefaults,
          includeStarred,
          includeWatching,
          onlyIncludeRepos: { values: parseLines(onlyIncludeRepos) },
          excludeRepos: { values: parseLines(excludeRepos) },
        },
      });
      if (token.trim()) {
        await configClient.replaceSourceToken({
          sourceId: source.id,
          tokenPlaintext: token,
        });
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] });
      toast.success('Source updated');
      onClose();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const loading = createMutation.isPending || updateMutation.isPending;

  return (
    <Modal
      title={form.mode === 'create' ? 'Add source' : 'Edit source'}
      onClose={onClose}
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (form.mode === 'create') createMutation.mutate();
          else updateMutation.mutate();
        }}
      >
        <Field label="GitHub Account">
          <Input
            value={username}
            onChange={setUsername}
            placeholder="octocat"
            required
          />
        </Field>
        <Field
          label={form.mode === 'create' ? 'Token' : 'Replace token'}
          help={<TokenHelpButton />}
        >
          <Input
            value={token}
            onChange={setTokenValue}
            type="password"
            required={form.mode === 'create'}
            placeholder={
              form.mode === 'create'
                ? 'ghp_...'
                : 'Leave blank to keep current token'
            }
          />
        </Field>
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">Repository selection</legend>
          <div className="grid gap-2 sm:grid-cols-3">
            <Checkbox
              label="Default"
              checked={includeDefaults}
              onChange={setIncludeDefaults}
            />
            <Checkbox
              label="Starred"
              checked={includeStarred}
              onChange={setIncludeStarred}
            />
            <Checkbox
              label="Watching"
              checked={includeWatching}
              onChange={setIncludeWatching}
            />
          </div>
        </fieldset>
        <Field label="Only include repos">
          <Textarea
            value={onlyIncludeRepos}
            onChange={setOnlyIncludeRepos}
            placeholder="my-org/*"
          />
        </Field>
        <Field label="Exclude repos">
          <Textarea
            value={excludeRepos}
            onChange={setExcludeRepos}
            placeholder="my-org/private-*"
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={loading}>
            <IconCheck size={16} />
            Save
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function SourceCard({
  source,
  onEdit,
  onDelete,
}: {
  source: Source;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <Panel padded>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <IconBrandGithub size={18} />
            <h3 className="truncate text-base font-semibold">
              {sourceLabel(source)}
            </h3>
          </div>
          <p className="mt-1 text-sm text-[#57606a]">{source.username}</p>
        </div>
        <Label tone={source.token ? 'success' : 'danger'}>
          {source.token ? 'token stored' : 'token missing'}
        </Label>
      </div>
      <div className="mt-4 flex flex-wrap gap-2 text-xs">
        {source.includeDefaults && <Label>default</Label>}
        {source.includeStarred && <Label>starred</Label>}
        {source.includeWatching && <Label>watching</Label>}
        {source.onlyIncludeRepos.length > 0 && (
          <Label>{source.onlyIncludeRepos.length} include rules</Label>
        )}
        {source.excludeRepos.length > 0 && (
          <Label>{source.excludeRepos.length} exclude rules</Label>
        )}
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button variant="secondary" onClick={onEdit}>
          Edit
        </Button>
        <Button variant="danger" onClick={onDelete}>
          Delete
        </Button>
      </div>
    </Panel>
  );
}

function TaskRow({ task, compact = false }: { task: Task; compact?: boolean }) {
  return (
    <button
      type="button"
      className={`w-full min-w-0 rounded-md px-4 py-3 text-left hover:bg-[#f6f8fa] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#0969da] ${compact ? 'cursor-default' : ''}`}
      onClick={() => !compact && navigate(`/maintenance/tasks/${task.taskId}`)}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <TaskStateBadge state={task.state} />
        <span className="font-medium">{task.name}</span>
        <span className="font-mono text-xs text-[#57606a]">{task.jobType}</span>
      </div>
      {task.progress?.summary && (
        <p className="mt-1 text-sm text-[#57606a]">{task.progress.summary}</p>
      )}
      <div className="mt-2 flex flex-wrap gap-3 text-xs text-[#57606a]">
        <span>Started {formatAbsolute(task.startedAt)}</span>
        {task.finishedAt && (
          <span>Finished {formatAbsolute(task.finishedAt)}</span>
        )}
      </div>
    </button>
  );
}

function RefRow({ refItem }: { refItem: RepoRef }) {
  return (
    <div className="flex min-w-0 items-center gap-3 px-4 py-3 text-sm">
      {refItem.refKind === 'head' ? (
        <IconGitBranch size={16} />
      ) : (
        <IconTag size={16} />
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-[#0969da]">
          {displayRefName(refItem.refName)}
        </div>
        <div className="truncate text-xs text-[#57606a]">
          {refItem.currentHash}
        </div>
      </div>
      <Label
        tone={refItem.status === 'active' ? 'success' : 'neutral'}
        className="shrink-0"
      >
        {refItem.status}
      </Label>
    </div>
  );
}

function ChangeRow({ change }: { change: RepoRefChange }) {
  return (
    <div className="px-4 py-3 text-sm">
      <div className="flex min-w-0 items-center gap-2">
        <IconCircleDot size={14} className="text-[#0969da]" />
        <span className="font-medium">{change.action}</span>
        <span className="min-w-0 truncate font-mono text-xs text-[#57606a]">
          {displayRefName(change.refName)}
        </span>
      </div>
      <p className="mt-1 text-xs text-[#57606a]">
        {formatAbsolute(change.createdAt)}
      </p>
    </div>
  );
}

function LoginScreen({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPasswordValue] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  return (
    <div className="flex min-h-screen items-center justify-center bg-[#f6f8fa] p-4">
      <form
        className="w-full max-w-sm rounded-lg border border-[#d0d7de] bg-white p-6 shadow-sm"
        onSubmit={async (event) => {
          event.preventDefault();
          setLoading(true);
          setError('');
          setToken(password.trim());
          try {
            await configClient.ping({});
            onSuccess();
          } catch {
            clearToken();
            setError('Invalid password');
          } finally {
            setLoading(false);
          }
        }}
      >
        <div className="mb-5 flex flex-col items-center gap-3 text-center">
          <div className="rounded-full border border-[#d0d7de] bg-[#f6f8fa] p-3">
            <IconLock size={24} />
          </div>
          <div>
            <h1 className="text-xl font-semibold">Git Plus</h1>
            <p className="mt-1 text-sm text-[#57606a]">
              Enter the dashboard password
            </p>
          </div>
        </div>
        <Field label="Password">
          <Input
            type="password"
            value={password}
            onChange={setPasswordValue}
            autoFocus
            required
          />
        </Field>
        {error && <p className="mt-2 text-sm text-[#cf222e]">{error}</p>}
        <Button
          type="submit"
          className="mt-4 w-full justify-center"
          loading={loading}
        >
          Unlock
        </Button>
      </form>
    </div>
  );
}

function StartupErrorScreen({ error }: { error: unknown }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-[#f6f8fa] p-4">
      <Panel className="w-full max-w-lg" padded>
        <StatusNote
          tone="danger"
          title="Git Plus could not start"
          detail={errorMessage(error)}
        />
        <Button className="mt-4" onClick={() => location.reload()}>
          <IconRefresh size={16} />
          Retry
        </Button>
      </Panel>
    </div>
  );
}

function PageFrame({
  title,
  description,
  actions,
  children,
}: {
  title: string;
  description?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="min-w-0">
      <div className="mb-5 flex flex-col gap-3 border-b border-[#d0d7de] pb-4 md:flex-row md:items-start md:justify-between">
        <div className="min-w-0">
          <h1 className="truncate text-2xl font-semibold tracking-normal">
            {title}
          </h1>
          {description && (
            <p className="mt-1 text-sm text-[#57606a]">{description}</p>
          )}
        </div>
        {actions && (
          <div className="flex w-full flex-wrap items-center gap-2 md:w-auto md:shrink-0 md:justify-end">
            {actions}
          </div>
        )}
      </div>
      {children}
    </div>
  );
}

function Panel({
  title,
  className = '',
  bodyClassName = '',
  padded = false,
  children,
}: {
  title?: string;
  className?: string;
  bodyClassName?: string;
  padded?: boolean;
  children: React.ReactNode;
}) {
  return (
    <section
      className={`min-w-0 rounded-md border border-[#d0d7de] bg-white ${className}`}
    >
      {title && (
        <div className="border-b border-[#d0d7de] bg-[#f6f8fa] px-4 py-2.5 text-sm font-semibold">
          {title}
        </div>
      )}
      <div className={`${padded ? 'p-4' : ''} ${bodyClassName}`}>
        {children}
      </div>
    </section>
  );
}

function Button({
  children,
  onClick,
  type = 'button',
  variant = 'primary',
  loading = false,
  className = '',
}: {
  children: React.ReactNode;
  onClick?: () => void;
  type?: 'button' | 'submit';
  variant?: 'primary' | 'secondary' | 'danger';
  loading?: boolean;
  className?: string;
}) {
  const styles = {
    primary: 'border-[#1f883d] bg-[#1f883d] text-white hover:bg-[#1a7f37]',
    secondary: 'border-[#d0d7de] bg-white text-[#1f2328] hover:bg-[#f6f8fa]',
    danger: 'border-[#d0d7de] bg-white text-[#cf222e] hover:bg-[#fff1f1]',
  };
  return (
    <button
      type={type === 'submit' ? 'submit' : 'button'}
      className={`inline-flex h-8 items-center gap-2 rounded-md border px-3 text-sm font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da] disabled:cursor-not-allowed disabled:opacity-60 ${styles[variant]} ${className}`}
      onClick={onClick}
      disabled={loading}
    >
      {loading ? (
        <span className="size-3 animate-spin rounded-full border-2 border-current border-t-transparent" />
      ) : null}
      {children}
    </button>
  );
}

function NavButton({
  active,
  icon: Icon,
  label,
  onClick,
}: {
  active: boolean;
  icon: typeof IconBook;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={`flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm ${
        active ? 'bg-[#0969da] text-white' : 'text-[#1f2328] hover:bg-[#f6f8fa]'
      } focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]`}
      onClick={onClick}
    >
      <Icon size={16} />
      {label}
    </button>
  );
}

function Field({
  label,
  help,
  children,
}: {
  label: string;
  help?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="block">
      <div className="mb-1 flex items-center gap-1 text-sm font-medium">
        <span>{label}</span>
        {help}
      </div>
      {children}
    </div>
  );
}

function TokenHelpButton() {
  const rootRef = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) return;

    const handlePointerDown = (event: PointerEvent) => {
      if (rootRef.current?.contains(event.target as Node)) return;
      setOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };

    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);

    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open]);

  return (
    <span ref={rootRef} className="relative inline-flex">
      <button
        type="button"
        aria-controls="github-token-help"
        aria-expanded={open}
        aria-label="GitHub token help"
        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-[#d0d7de] bg-white text-[#57606a] hover:border-[#0969da] hover:text-[#0969da] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          setOpen((value) => !value);
        }}
      >
        <IconHelpCircle size={14} />
      </button>
      {open ? (
        <span
          id="github-token-help"
          role="tooltip"
          className="absolute left-0 top-7 z-[60] w-72 max-w-[calc(100vw-3rem)] rounded-md border border-[#d0d7de] bg-white p-3 text-sm font-normal leading-5 text-[#1f2328] shadow-lg"
        >
          Create a GitHub token at{' '}
          <a
            href={GITHUB_TOKEN_URL}
            target="_blank"
            rel="noreferrer"
            className="text-[#0969da] underline underline-offset-2"
          >
            https://github.com/settings/tokens/new
          </a>
          .
        </span>
      ) : null}
    </span>
  );
}

function Input({
  value,
  onChange,
  type = 'text',
  placeholder,
  min,
  required,
  autoFocus,
  className = '',
}: {
  value: string;
  onChange: (value: string) => void;
  type?: string;
  placeholder?: string;
  min?: number;
  required?: boolean;
  autoFocus?: boolean;
  className?: string;
}) {
  return (
    <input
      className={`h-8 w-full rounded-md border border-[#d0d7de] bg-white px-3 text-sm outline-none focus:border-[#0969da] focus:ring-2 focus:ring-[#0969da]/20 ${className}`}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      type={type}
      placeholder={placeholder}
      min={min}
      required={required}
      autoFocus={autoFocus}
    />
  );
}

function Textarea({
  value,
  onChange,
  placeholder,
  className = '',
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <textarea
      className={`min-h-24 w-full rounded-md border border-[#d0d7de] bg-white px-3 py-2 text-sm outline-none focus:border-[#0969da] focus:ring-2 focus:ring-[#0969da]/20 ${className}`}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      placeholder={placeholder}
    />
  );
}

function Select({
  value,
  onChange,
  options,
  className = '',
  ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
  className?: string;
  ariaLabel?: string;
}) {
  return (
    <select
      className={`h-8 rounded-md border border-[#d0d7de] bg-white px-2 text-sm outline-none focus:border-[#0969da] focus:ring-2 focus:ring-[#0969da]/20 ${className}`}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      aria-label={ariaLabel}
    >
      {options.map((option) => (
        <option key={`${option.value}-${option.label}`} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  );
}

function SearchBox({
  value,
  onChange,
  placeholder,
  ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  ariaLabel: string;
}) {
  return (
    <div className="relative">
      <IconSearch size={16} className="absolute left-3 top-2 text-[#57606a]" />
      <input
        className="h-8 w-full rounded-md border border-[#d0d7de] bg-white pl-9 pr-9 text-sm outline-none focus:border-[#0969da] focus:ring-2 focus:ring-[#0969da]/20"
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
        placeholder={placeholder}
        aria-label={ariaLabel}
      />
      {value && (
        <button
          type="button"
          className="absolute right-1 top-0.5 flex size-7 items-center justify-center rounded text-[#57606a] hover:bg-[#f6f8fa] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]"
          onClick={() => onChange('')}
          aria-label="Clear search"
        >
          <IconX size={15} />
        </button>
      )}
    </div>
  );
}

function Checkbox({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2 rounded-md border border-[#d0d7de] px-3 py-2 text-sm">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.currentTarget.checked)}
      />
      {label}
    </label>
  );
}

function Segmented({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string; icon: typeof IconBook }>;
}) {
  return (
    <div className="inline-flex rounded-md border border-[#d0d7de] bg-white p-1">
      {options.map((option) => {
        const Icon = option.icon;
        return (
          <button
            key={option.value}
            type="button"
            className={`flex items-center gap-1.5 rounded px-3 py-1.5 text-sm ${
              value === option.value
                ? 'bg-[#0969da] text-white'
                : 'hover:bg-[#f6f8fa]'
            } focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]`}
            onClick={() => onChange(option.value)}
          >
            <Icon size={15} />
            {option.label}
          </button>
        );
      })}
    </div>
  );
}

function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-[#1f2328]/35 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-lg border border-[#d0d7de] bg-white shadow-xl"
      >
        <div className="flex items-center justify-between border-b border-[#d0d7de] px-4 py-3">
          <h2 className="font-semibold">{title}</h2>
          <button
            type="button"
            className="rounded-md p-1 hover:bg-[#f6f8fa] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#0969da]"
            onClick={onClose}
            aria-label="Close"
          >
            <IconX size={18} />
          </button>
        </div>
        <div className="p-4">{children}</div>
      </div>
    </div>
  );
}

function Label({
  children,
  tone = 'neutral',
  className = '',
}: {
  children: React.ReactNode;
  tone?: 'neutral' | 'success' | 'warning' | 'danger';
  className?: string;
}) {
  const styles = {
    neutral: 'border-[#d0d7de] text-[#57606a]',
    success: 'border-[#2da44e66] bg-[#dafbe1] text-[#1a7f37]',
    warning: 'border-[#bf870066] bg-[#fff8c5] text-[#9a6700]',
    danger: 'border-[#cf222e66] bg-[#ffebe9] text-[#cf222e]',
  };
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs ${styles[tone]} ${className}`}
    >
      {children}
    </span>
  );
}

function StatusNote({
  tone,
  title,
  detail,
}: {
  tone: 'success' | 'danger' | 'neutral';
  title: string;
  detail?: string;
}) {
  const Icon =
    tone === 'success'
      ? IconCheck
      : tone === 'danger'
        ? IconAlertTriangle
        : IconCircleDot;
  const styles = {
    success: 'border-[#2da44e66] bg-[#dafbe1] text-[#1a7f37]',
    danger: 'border-[#cf222e66] bg-[#ffebe9] text-[#cf222e]',
    neutral: 'border-[#d0d7de] bg-[#f6f8fa] text-[#57606a]',
  };
  return (
    <div className={`rounded-md border p-3 ${styles[tone]}`}>
      <div className="flex gap-2">
        <Icon size={18} className="mt-0.5 shrink-0" />
        <div>
          <p className="text-sm font-medium">{title}</p>
          {detail && <p className="mt-1 text-xs opacity-80">{detail}</p>}
        </div>
      </div>
    </div>
  );
}

function EmptyState({
  icon,
  title,
  description,
  actionLabel,
  onAction,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="flex flex-col items-center px-6 py-14 text-center">
      <div className="rounded-full border border-[#d0d7de] bg-[#f6f8fa] p-3 text-[#57606a]">
        {icon}
      </div>
      <h2 className="mt-4 text-base font-semibold">{title}</h2>
      <p className="mt-1 max-w-md text-sm text-[#57606a]">{description}</p>
      {actionLabel && onAction && (
        <Button className="mt-4" onClick={onAction}>
          <IconPlus size={16} />
          {actionLabel}
        </Button>
      )}
    </div>
  );
}

function TaskStateBadge({ state }: { state: TaskState }) {
  if (state === TaskState.RUNNING) return <Label tone="warning">running</Label>;
  if (state === TaskState.FINISHED)
    return <Label tone="success">finished</Label>;
  if (state === TaskState.FAILED) return <Label tone="danger">failed</Label>;
  return <Label>queued</Label>;
}

function ListSkeleton() {
  return (
    <div className="space-y-3 p-4">
      {Array.from({ length: 4 }).map((_, index) => (
        <div
          key={index}
          className="h-16 animate-pulse rounded-md bg-[#f6f8fa]"
        />
      ))}
    </div>
  );
}

function LoadingScreen() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-[#f6f8fa]">
      <span className="size-8 animate-spin rounded-full border-2 border-[#0969da] border-t-transparent" />
    </div>
  );
}

function NotFound() {
  return (
    <Panel>
      <EmptyState
        icon={<IconAlertTriangle size={28} />}
        title="Not found"
        description="The requested resource is unavailable."
        actionLabel="Back to repositories"
        onAction={() => navigate('/repos')}
      />
    </Panel>
  );
}

function NotFoundPage() {
  return (
    <PageFrame
      title="Page not found"
      description="The requested page is unavailable."
    >
      <NotFound />
    </PageFrame>
  );
}

function useRoute(): Route {
  const [locationKey, setLocationKey] = useState(
    () => `${location.pathname}${location.search}`,
  );
  useEffect(() => {
    const listener = () =>
      setLocationKey(`${location.pathname}${location.search}`);
    addEventListener('popstate', listener);
    return () => removeEventListener('popstate', listener);
  }, []);
  const [path, queryString = ''] = locationKey.split('?');
  const search = new URLSearchParams(queryString).get('q') ?? '';

  if (path.startsWith('/repos/') && path !== '/repos') {
    return { name: 'repo', id: path.split('/')[2] ?? '' };
  }
  if (path === '/config') return { name: 'config' };
  if (path === '/config/sources') return { name: 'sources' };
  if (path === '/config/cron') return { name: 'cron' };
  if (path.startsWith('/maintenance/tasks/') && path !== '/maintenance/tasks') {
    return { name: 'task', id: path.split('/')[3] ?? '' };
  }
  if (path === '/maintenance/tasks') return { name: 'tasks' };
  if (path === '/repos') return { name: 'repos', search };
  if (path === '/' || path === '') return { name: 'repos' };
  return { name: 'not-found' };
}

function navigate(path: string) {
  history.pushState(null, '', path);
  dispatchEvent(new PopStateEvent('popstate'));
}

function isActiveNav(route: Route, path: string): boolean {
  if (path === '/repos') return route.name === 'repos' || route.name === 'repo';
  if (path === '/maintenance/tasks')
    return route.name === 'tasks' || route.name === 'task';
  if (path === '/config/sources') return route.name === 'sources';
  if (path === '/config/cron') return route.name === 'cron';
  if (path === '/config') return route.name === 'config';
  return false;
}

function sourceLabel(source: Source): string {
  return source.username || source.id;
}

function parseLines(value: string): Array<string> {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function formatRelative(
  ts: Parameters<typeof timestampDate>[0] | undefined,
): string {
  if (!ts) return 'never';
  return dayjs(timestampDate(ts)).fromNow();
}

function formatAbsolute(
  ts: Parameters<typeof timestampDate>[0] | undefined,
): string {
  if (!ts) return '—';
  return dayjs(timestampDate(ts)).format('YYYY-MM-DD HH:mm:ss');
}

function displayRefName(refName: string): string {
  return refName.replace(/^refs\/(heads|tags)\//, '');
}

function enqueueResultLabel(result: TaskEnqueueResult): string {
  if (result === TaskEnqueueResult.STARTED) return 'started';
  if (result === TaskEnqueueResult.QUEUED) return 'queued';
  if (result === TaskEnqueueResult.DEDUPED) return 'already queued';
  return 'enqueued';
}

function errorMessage(error: unknown): string {
  if (error instanceof ConnectError) return error.message;
  if (error instanceof Error) return error.message;
  return 'Unexpected error';
}
