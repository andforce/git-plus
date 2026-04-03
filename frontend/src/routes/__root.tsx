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
import type { QueryClient } from '@tanstack/react-query';
import { appCssVariablesResolver, appTheme } from '~ui/theme';

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  head: () => ({
    meta: [
      {
        title: 'Frontend SPA Template',
      },
    ],
  }),
  component: RootLayout,
});

function RootLayout() {
  return (
    <>
      <HeadContent />
      <MantineProvider
        theme={appTheme}
        cssVariablesResolver={appCssVariablesResolver}
        defaultColorScheme="light"
      >
        <ModalsProvider>
          <Outlet />
        </ModalsProvider>

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
