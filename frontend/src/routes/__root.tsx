import {
  HeadContent,
  Outlet,
  createRootRouteWithContext,
} from '@tanstack/react-router';
import { TanStackRouterDevtoolsPanel } from '@tanstack/react-router-devtools';
import { ReactQueryDevtoolsPanel } from '@tanstack/react-query-devtools';
import { TanStackDevtools } from '@tanstack/react-devtools';
import { MantineProvider } from '@mantine/core';
import { Toaster } from 'sonner';
import { NavigationProgress } from '@mantine/nprogress';
import { ModalsProvider } from '@mantine/modals';
import { Code, ConnectError } from '@connectrpc/connect';
import { router } from '../router';
import type { QueryClient } from '@tanstack/react-query';
import { appCssVariablesResolver, appTheme } from '~ui/theme';
import { AuthGuard, AuthOverlay } from '~components/AuthGuard';
import { configClient } from '~lib/connect/client';

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  beforeLoad: async () => {
    try {
      await configClient.ping({});
      return { isAuthenticated: true };
    } catch (error) {
      if (
        error instanceof ConnectError &&
        error.code === Code.Unauthenticated
      ) {
        return { isAuthenticated: false };
      }
      throw error;
    }
  },
  head: () => ({
    meta: [
      {
        title: 'Git Plus',
      },
    ],
  }),
  component: RootLayout,
});

function RootLayout() {
  const { isAuthenticated } = Route.useRouteContext();

  return (
    <>
      <HeadContent />
      <MantineProvider
        theme={appTheme}
        cssVariablesResolver={appCssVariablesResolver}
        defaultColorScheme="light"
      >
        {isAuthenticated ? (
          <>
            <ModalsProvider>
              <Outlet />
            </ModalsProvider>
            <AuthGuard />
          </>
        ) : (
          <AuthOverlay onSuccess={() => router.invalidate()} />
        )}

        <Toaster position="top-center" richColors />
        <NavigationProgress />
        <TanStackDevtools
          config={{
            position: 'bottom-right',
          }}
          plugins={[
            {
              name: 'TanStack Router',
              render: <TanStackRouterDevtoolsPanel />,
            },
            {
              name: 'React Query',
              render: <ReactQueryDevtoolsPanel />,
            },
          ]}
        />
      </MantineProvider>
    </>
  );
}
