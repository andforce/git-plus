import { createFileRoute, redirect } from '@tanstack/react-router';

export const Route = createFileRoute('/_dashboard/maintenance/')({
  beforeLoad: () => {
    throw redirect({ to: '/maintenance/tasks' });
  },
});
