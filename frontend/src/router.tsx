import { createRouter } from '@tanstack/react-router';
import { nprogress } from '@mantine/nprogress';
import { QueryClient } from '@tanstack/react-query';

// Import the generated route tree
import { routeTree } from './routeTree.gen';
import { NotFoundElement } from '~components/system/not-found.tsx';

export const queryClient = new QueryClient();

// Create a router instance for the SPA runtime.
export const getRouter = () => {
  const router = createRouter({
    routeTree,
    context: { queryClient },
    scrollRestoration: true,
    defaultPreloadStaleTime: 0,
    defaultPreload: 'intent',
    defaultNotFoundComponent: NotFoundElement,
  });

  router.subscribe('onBeforeLoad', ({ fromLocation, pathChanged }) => {
    fromLocation && pathChanged && nprogress.start();
  });
  router.subscribe('onLoad', () => {
    nprogress.complete();
  });

  return router;
};

export const router = getRouter();

// Register the router instance for type safety
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
